// Package firecracker manages Firecracker processes and their cgroup-v2
// placement. The manager does not require a running Firecracker daemon until
// StartVM is called.
package firecracker

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const defaultStopTimeout = 5 * time.Second

// Process is the small process surface used by FirecrackerManager. It makes
// lifecycle tests independent of a Firecracker binary and the host cgroup.
type Process interface {
	Start() error
	PID() int
	Signal(os.Signal) error
	Wait() error
}

// FileSystem is the cgroup filesystem surface used by the manager.
type FileSystem interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(path string, data []byte, perm os.FileMode) error
	RemoveAll(path string) error
}

type osFileSystem struct{}

func (osFileSystem) MkdirAll(p string, m os.FileMode) error            { return os.MkdirAll(p, m) }
func (osFileSystem) WriteFile(p string, b []byte, m os.FileMode) error { return os.WriteFile(p, b, m) }
func (osFileSystem) RemoveAll(p string) error                          { return os.RemoveAll(p) }

type execProcess struct{ cmd *exec.Cmd }

func (p *execProcess) Start() error { return p.cmd.Start() }
func (p *execProcess) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
func (p *execProcess) Signal(s os.Signal) error { return p.cmd.Process.Signal(s) }
func (p *execProcess) Wait() error              { return p.cmd.Wait() }

// Options controls the external seams and timing of a manager. Zero values
// use the Firecracker executable, the host filesystem, and a five-second stop
// grace period.
type Options struct {
	Command     string
	StopTimeout time.Duration
	FS          FileSystem
	NewProcess  func(command string, args ...string) (Process, error)
}

type managedProcess struct {
	process Process
	cgroup  string
}

// FirecrackerManager tracks one process per VM ID. All access to processes is
// synchronized; process waits and filesystem operations happen outside the
// mutex so a slow VM cannot block unrelated ListVMs calls.
type FirecrackerManager struct {
	mu          sync.RWMutex
	processes   map[string]managedProcess
	starting    map[string]chan struct{}
	cgroupBase  string
	command     string
	stopTimeout time.Duration
	fs          FileSystem
	newProcess  func(string, ...string) (Process, error)
}

// NewFirecrackerManager creates a manager rooted at cgroupBase.
func NewFirecrackerManager(cgroupBase string) *FirecrackerManager {
	return NewFirecrackerManagerWithOptions(cgroupBase, Options{})
}

// NewFirecrackerManagerWithOptions creates a manager with testable seams.
func NewFirecrackerManagerWithOptions(cgroupBase string, opts Options) *FirecrackerManager {
	if opts.Command == "" {
		opts.Command = "firecracker"
	}
	if opts.StopTimeout <= 0 {
		opts.StopTimeout = defaultStopTimeout
	}
	if opts.FS == nil {
		opts.FS = osFileSystem{}
	}
	if opts.NewProcess == nil {
		opts.NewProcess = func(command string, args ...string) (Process, error) {
			cmd := exec.Command(command, args...)
			cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
			return &execProcess{cmd: cmd}, nil
		}
	}
	return &FirecrackerManager{processes: make(map[string]managedProcess), starting: make(map[string]chan struct{}), cgroupBase: filepath.Clean(cgroupBase), command: opts.Command, stopTimeout: opts.StopTimeout, fs: opts.FS, newProcess: opts.NewProcess}
}

// StartVM starts Firecracker, places its PID in a dedicated cgroup, and then
// publishes it in the manager. Any failure terminates the process and removes
// the newly-created cgroup before returning.
func (m *FirecrackerManager) StartVM(id, socketPath, configPath string) error {
	if err := validID(id); err != nil {
		return err
	}
	m.mu.Lock()
	if _, exists := m.processes[id]; exists {
		m.mu.Unlock()
		return fmt.Errorf("VM %q is already running", id)
	}
	if _, exists := m.starting[id]; exists {
		m.mu.Unlock()
		return fmt.Errorf("VM %q is already starting", id)
	}
	startDone := make(chan struct{})
	m.starting[id] = startDone
	m.mu.Unlock()
	cgroup := filepath.Join(m.cgroupBase, id)
	started := false
	defer func() {
		m.mu.Lock()
		delete(m.starting, id)
		close(startDone)
		m.mu.Unlock()
	}()
	defer func() {
		if !started {
			// Remove partially-created cgroups too, including when MkdirAll
			// itself reports an error after creating parent directories.
			_ = m.fs.RemoveAll(cgroup)
		}
	}()
	if err := m.fs.MkdirAll(cgroup, 0755); err != nil {
		return fmt.Errorf("create cgroup for %q: %w", id, err)
	}
	proc, err := m.newProcess(m.command, "--api-sock", socketPath, "--config-file", configPath)
	if err != nil {
		return fmt.Errorf("create Firecracker process: %w", err)
	}
	defer func() {
		if !started {
			// A process that failed before Start has no PID and must not be
			// signalled. In particular, exec.Cmd.Signal would panic here.
			if proc.PID() > 0 {
				_ = proc.Signal(syscall.SIGKILL)
				// A broken process seam must not make a failed start hang
				// forever. The real process is expected to reap promptly after
				// SIGKILL; the timeout protects the manager if it does not.
				wait := make(chan struct{})
				go func() {
					_ = proc.Wait()
					close(wait)
				}()
				timer := time.NewTimer(m.stopTimeout)
				select {
				case <-wait:
				case <-timer.C:
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}
		}
	}()
	if err := proc.Start(); err != nil {
		return fmt.Errorf("start Firecracker: %w", err)
	}
	pid := proc.PID()
	if pid <= 0 {
		return errors.New("Firecracker started without a valid PID")
	}
	if err := m.fs.WriteFile(filepath.Join(cgroup, "cgroup.procs"), []byte(fmt.Sprintf("%d\n", pid)), 0644); err != nil {
		return fmt.Errorf("place VM %q in cgroup: %w", id, err)
	}
	m.mu.Lock()
	if _, exists := m.processes[id]; exists {
		m.mu.Unlock()
		return fmt.Errorf("VM %q is already running", id)
	}
	m.processes[id] = managedProcess{process: proc, cgroup: cgroup}
	m.mu.Unlock()
	started = true
	return nil
}

// StopVM gracefully stops a VM, escalating to SIGKILL after the configured
// grace period. It is idempotent and always attempts cgroup cleanup.
func (m *FirecrackerManager) StopVM(id string) error {
	// A stop racing with a start must not return before the start has either
	// published a process or cleaned up its failed attempt. Otherwise the
	// just-started VM could be left running after a successful StopVM call.
	for {
		m.mu.Lock()
		entry, ok := m.processes[id]
		if ok {
			delete(m.processes, id)
			m.mu.Unlock()
			return m.stopProcess(id, entry)
		}
		startDone, starting := m.starting[id]
		m.mu.Unlock()
		if !starting {
			return nil
		}
		<-startDone
	}
}

func (m *FirecrackerManager) stopProcess(id string, entry managedProcess) error {
	wait := make(chan error, 1)
	go func() { wait <- entry.process.Wait() }()
	_ = entry.process.Signal(syscall.SIGTERM)
	timer := time.NewTimer(m.stopTimeout)
	defer timer.Stop()
	select {
	case <-wait:
	case <-timer.C:
		_ = entry.process.Signal(syscall.SIGKILL)
		// SIGKILL is not expected to fail for a live OS process. Do not
		// wait a second grace period here: StopVM has a bounded contract.
		select {
		case <-wait:
		default:
		}
	}
	if err := m.fs.RemoveAll(entry.cgroup); err != nil {
		return fmt.Errorf("remove cgroup for %q: %w", id, err)
	}
	return nil
}

// ListVMs returns a stable snapshot of currently tracked VM IDs.
func (m *FirecrackerManager) ListVMs() []string {
	m.mu.RLock()
	ids := make([]string, 0, len(m.processes))
	for id := range m.processes {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	sort.Strings(ids)
	return ids
}

func validID(id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\\`) {
		return fmt.Errorf("invalid VM ID %q", id)
	}
	return nil
}
