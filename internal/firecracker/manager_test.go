package firecracker

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

type testFS struct {
	mu        sync.Mutex
	files     map[string][]byte
	dirs      map[string]bool
	failWrite bool
}

func newTestFS() *testFS { return &testFS{files: map[string][]byte{}, dirs: map[string]bool{}} }
func (f *testFS) MkdirAll(p string, _ os.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dirs[p] = true
	return nil
}
func (f *testFS) WriteFile(p string, b []byte, _ os.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWrite {
		return errors.New("permission denied")
	}
	f.files[p] = append([]byte(nil), b...)
	return nil
}
func (f *testFS) RemoveAll(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.dirs, p)
	for name := range f.files {
		if name == p || len(name) > len(p) && name[:len(p)] == p {
			delete(f.files, name)
		}
	}
	return nil
}

type testProcess struct {
	pid     int
	started bool
	wait    chan error
	mu      sync.Mutex
	signals []os.Signal
}

func (p *testProcess) Start() error { p.mu.Lock(); p.started = true; p.mu.Unlock(); return nil }
func (p *testProcess) PID() int     { return p.pid }
func (p *testProcess) Signal(s os.Signal) error {
	p.mu.Lock()
	p.signals = append(p.signals, s)
	p.mu.Unlock()
	if s == syscall.SIGTERM {
		select {
		case p.wait <- nil:
		default:
		}
	}
	return nil
}
func (p *testProcess) Wait() error { return <-p.wait }

func TestStartStopVMAndList(t *testing.T) {
	fs := newTestFS()
	proc := &testProcess{pid: 1234, wait: make(chan error, 1)}
	m := NewFirecrackerManagerWithOptions("/cg", Options{FS: fs, StopTimeout: 20 * time.Millisecond, NewProcess: func(_ string, _ ...string) (Process, error) { return proc, nil }})
	if err := m.StartVM("vm-a", "/tmp/a.sock", "/tmp/a.json"); err != nil {
		t.Fatal(err)
	}
	if got := m.ListVMs(); len(got) != 1 || got[0] != "vm-a" {
		t.Fatalf("ListVMs() = %#v", got)
	}
	if got := string(fs.files[filepath.Join("/cg", "vm-a", "cgroup.procs")]); got != "1234\n" {
		t.Fatalf("pid file = %q", got)
	}
	if err := m.StopVM("vm-a"); err != nil {
		t.Fatal(err)
	}
	if got := m.ListVMs(); len(got) != 0 {
		t.Fatalf("ListVMs after stop = %#v", got)
	}
	if err := m.StopVM("vm-a"); err != nil {
		t.Fatal(err)
	}
}

func TestStartVMRemovesCgroupOnPlacementFailure(t *testing.T) {
	fs := newTestFS()
	fs.failWrite = true
	proc := &testProcess{pid: 9, wait: make(chan error, 1)}
	m := NewFirecrackerManagerWithOptions("/cg", Options{FS: fs, NewProcess: func(_ string, _ ...string) (Process, error) { return proc, nil }})
	if err := m.StartVM("vm", "sock", "config"); err == nil {
		t.Fatal("expected cgroup placement error")
	}
	if len(m.ListVMs()) != 0 {
		t.Fatal("failed start was published")
	}
	if fs.dirs[filepath.Join("/cg", "vm")] {
		t.Fatal("failed cgroup was not removed")
	}
}

func TestStartVMReportsMissingBinary(t *testing.T) {
	fs := newTestFS()
	m := NewFirecrackerManagerWithOptions("/cg", Options{FS: fs, NewProcess: func(_ string, _ ...string) (Process, error) { return nil, errors.New("executable not found") }})
	if err := m.StartVM("vm", "sock", "config"); err == nil {
		t.Fatal("expected process creation error")
	}
	if fs.dirs[filepath.Join("/cg", "vm")] {
		t.Fatal("cgroup left after process creation failure")
	}
}

func TestConcurrentListAndStartStop(t *testing.T) {
	fs := newTestFS()
	m := NewFirecrackerManagerWithOptions("/cg", Options{FS: fs, NewProcess: func(_ string, _ ...string) (Process, error) {
		return &testProcess{pid: 1, wait: make(chan error, 1)}, nil
	}})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "vm" + string(rune('a'+i))
			_ = m.StartVM(id, "sock", "config")
			_ = m.StopVM(id)
			_ = m.ListVMs()
		}(i)
	}
	wg.Wait()
}
