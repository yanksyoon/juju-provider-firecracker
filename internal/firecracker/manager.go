// Package firecracker manages Firecracker machines and their lifecycle.
package firecracker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	sdk "github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
)

const defaultStopTimeout = 5 * time.Second

type FileSystem interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(path string, data []byte, perm os.FileMode) error
	RemoveAll(path string) error
}
type osFileSystem struct{}

func (osFileSystem) MkdirAll(p string, m os.FileMode) error            { return os.MkdirAll(p, m) }
func (osFileSystem) WriteFile(p string, b []byte, m os.FileMode) error { return os.WriteFile(p, b, m) }
func (osFileSystem) RemoveAll(p string) error                          { return os.RemoveAll(p) }

type machineFactory func(context.Context, sdk.Config) (sdk.MachineIface, error)

// VMRequest is the validated, typed input to the SDK adapter.
type VMRequest struct {
	ID         string
	SocketPath string
	KernelPath string
	RootFSPath string
	TapName    string
	MACAddress string
	IP         net.IPNet
	VCPU       int64
	MemoryMiB  int64
	KernelArgs string
}

// SDKConfig maps a request to the pinned SDK configuration without invoking CNI.
func SDKConfig(r VMRequest) (sdk.Config, error) {
	if r.ID == "" || r.SocketPath == "" || r.KernelPath == "" || r.RootFSPath == "" || r.TapName == "" {
		return sdk.Config{}, errors.New("VM request requires ID, socket, kernel, rootfs, and tap paths")
	}
	if r.VCPU < 1 || r.MemoryMiB < 1 {
		return sdk.Config{}, errors.New("VM request requires positive vCPU and memory")
	}
	if r.MACAddress == "" {
		return sdk.Config{}, errors.New("VM request requires a MAC address")
	}
	drive := sdk.NewDrivesBuilder(r.RootFSPath).Build()
	iface := sdk.NetworkInterface{StaticConfiguration: &sdk.StaticNetworkConfiguration{HostDevName: r.TapName, MacAddress: r.MACAddress}}
	if r.IP.IP != nil {
		iface.StaticConfiguration.IPConfiguration = &sdk.IPConfiguration{IPAddr: r.IP}
	}
	return sdk.Config{VMID: r.ID, SocketPath: r.SocketPath, KernelImagePath: r.KernelPath, KernelArgs: r.KernelArgs,
		Drives: drive, NetworkInterfaces: sdk.NetworkInterfaces{iface}, MachineCfg: models.MachineConfiguration{VcpuCount: sdk.Int64(r.VCPU), MemSizeMib: sdk.Int64(r.MemoryMiB)}}, nil
}

type managedMachine struct {
	machine    sdk.MachineIface
	cgroup     string
	waitDone   chan error
	waitCancel context.CancelFunc
}

type Options struct {
	StopTimeout     time.Duration
	ShutdownTimeout time.Duration
	FS              FileSystem
	NewMachine      machineFactory
}

type FirecrackerManager struct {
	mu                           sync.RWMutex
	machines                     map[string]managedMachine
	starting                     map[string]chan struct{}
	cgroupBase                   string
	stopTimeout, shutdownTimeout time.Duration
	fs                           FileSystem
	newMachine                   machineFactory
}

func NewFirecrackerManager(cgroupBase string) *FirecrackerManager {
	return NewFirecrackerManagerWithOptions(cgroupBase, Options{})
}
func NewFirecrackerManagerWithOptions(cgroupBase string, opts Options) *FirecrackerManager {
	if opts.StopTimeout <= 0 {
		opts.StopTimeout = defaultStopTimeout
	}
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = opts.StopTimeout
	}
	if opts.FS == nil {
		opts.FS = osFileSystem{}
	}
	if opts.NewMachine == nil {
		opts.NewMachine = func(ctx context.Context, cfg sdk.Config) (sdk.MachineIface, error) { return sdk.NewMachine(ctx, cfg) }
	}
	return &FirecrackerManager{machines: map[string]managedMachine{}, starting: map[string]chan struct{}{}, cgroupBase: filepath.Clean(cgroupBase), stopTimeout: opts.StopTimeout, shutdownTimeout: opts.ShutdownTimeout, fs: opts.FS, newMachine: opts.NewMachine}
}

func (m *FirecrackerManager) StartVM(ctx context.Context, r VMRequest) error {
	if err := validID(r.ID); err != nil {
		return err
	}
	cfg, err := SDKConfig(r)
	if err != nil {
		return fmt.Errorf("map VM %q to SDK config: %w", r.ID, err)
	}
	m.mu.Lock()
	if _, ok := m.machines[r.ID]; ok {
		m.mu.Unlock()
		return fmt.Errorf("VM %q is already running", r.ID)
	}
	if _, ok := m.starting[r.ID]; ok {
		m.mu.Unlock()
		return fmt.Errorf("VM %q is already starting", r.ID)
	}
	done := make(chan struct{})
	m.starting[r.ID] = done
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.starting, r.ID); close(done); m.mu.Unlock() }()
	cgroup := filepath.Join(m.cgroupBase, r.ID)
	if err := m.fs.MkdirAll(cgroup, 0755); err != nil {
		return fmt.Errorf("create cgroup for %q: %w", r.ID, err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = m.fs.RemoveAll(cgroup)
		}
	}()
	machine, err := m.newMachine(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create VM %q: %w", r.ID, err)
	}
	if err := machine.Start(ctx); err != nil {
		_ = machine.StopVMM()
		return fmt.Errorf("start VM %q: %w", r.ID, err)
	}
	waitCtx, waitCancel := context.WithCancel(context.Background())
	waitDone := make(chan error, 1)
	go func() { waitDone <- machine.Wait(waitCtx) }()
	m.mu.Lock()
	m.machines[r.ID] = managedMachine{machine: machine, cgroup: cgroup, waitDone: waitDone, waitCancel: waitCancel}
	m.mu.Unlock()
	cleanup = false
	return nil
}

func (m *FirecrackerManager) StopVM(id string) error {
	for {
		m.mu.Lock()
		entry, ok := m.machines[id]
		if ok {
			delete(m.machines, id)
			m.mu.Unlock()
			return m.stopMachine(id, entry)
		}
		wait, starting := m.starting[id]
		m.mu.Unlock()
		if !starting {
			return nil
		}
		<-wait
	}
}
func (m *FirecrackerManager) stopMachine(id string, e managedMachine) error {
	var first error
	if e.waitCancel != nil {
		defer e.waitCancel()
	}
	if e.machine != nil {
		ctx, cancel := context.WithTimeout(context.Background(), m.shutdownTimeout)
		err := e.machine.Shutdown(ctx)
		cancel()
		if err != nil {
			first = fmt.Errorf("shutdown VM %q: %w", id, err)
		}
		if err := e.machine.StopVMM(); err != nil && first == nil {
			first = fmt.Errorf("stop VM %q: %w", id, err)
		}
		ctx, cancel = context.WithTimeout(context.Background(), m.stopTimeout)
		if e.waitDone != nil {
			select {
			case err := <-e.waitDone:
				if err != nil && first == nil {
					first = fmt.Errorf("wait VM %q: %w", id, err)
				}
			case <-ctx.Done():
				if first == nil {
					first = fmt.Errorf("wait VM %q: %w", id, ctx.Err())
				}
			}
		}
		cancel()
	}
	if err := m.fs.RemoveAll(e.cgroup); err != nil && first == nil {
		first = fmt.Errorf("remove cgroup for %q: %w", id, err)
	}
	return first
}
func (m *FirecrackerManager) ListVMs() []string {
	m.mu.RLock()
	ids := make([]string, 0, len(m.machines))
	for id := range m.machines {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	sort.Strings(ids)
	return ids
}
func validID(id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("invalid VM ID %q", id)
	}
	return nil
}
