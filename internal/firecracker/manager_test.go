package firecracker

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	sdk "github.com/firecracker-microvm/firecracker-go-sdk"
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

func TestSDKConfigMapsTypedRequest(t *testing.T) {
	_, network, _ := net.ParseCIDR("192.0.2.10/24")
	cfg, err := SDKConfig(VMRequest{ID: "vm", SocketPath: "/run/vm.sock", KernelPath: "/boot/vmlinux", RootFSPath: "/var/lib/vm.ext4", TapName: "tap0", MACAddress: "02:00:00:00:00:01", IP: *network, VCPU: 2, MemoryMiB: 1024, KernelArgs: "console=ttyS0"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VMID != "vm" || cfg.SocketPath != "/run/vm.sock" || cfg.KernelImagePath != "/boot/vmlinux" || cfg.KernelArgs != "console=ttyS0" {
		t.Fatalf("unexpected SDK config: %+v", cfg)
	}
	if len(cfg.Drives) != 1 || sdk.StringValue(cfg.Drives[0].PathOnHost) != "/var/lib/vm.ext4" || !sdk.BoolValue(cfg.Drives[0].IsRootDevice) {
		t.Fatalf("unexpected drives: %+v", cfg.Drives)
	}
	if cfg.MachineCfg.VcpuCount == nil || sdk.Int64Value(cfg.MachineCfg.VcpuCount) != 2 || sdk.Int64Value(cfg.MachineCfg.MemSizeMib) != 1024 {
		t.Fatalf("unexpected machine sizing: %+v", cfg.MachineCfg)
	}
	if cfg.NetworkInterfaces[0].StaticConfiguration.HostDevName != "tap0" || cfg.NetworkInterfaces[0].StaticConfiguration.MacAddress != "02:00:00:00:00:01" {
		t.Fatalf("unexpected network: %+v", cfg.NetworkInterfaces)
	}
}

type fakeMachine struct {
	sdk.MachineIface
	started, shutdown, stopped, waited bool
}

func (m *fakeMachine) Start(context.Context) error    { m.started = true; return nil }
func (m *fakeMachine) Shutdown(context.Context) error { m.shutdown = true; return nil }
func (m *fakeMachine) StopVMM() error                 { m.stopped = true; return nil }
func (m *fakeMachine) Wait(context.Context) error     { m.waited = true; return nil }

func TestSDKStartStopUsesMachineAndCleansCgroup(t *testing.T) {
	fs := newTestFS()
	machine := &fakeMachine{}
	m := NewFirecrackerManagerWithOptions("/cg", Options{FS: fs, NewMachine: func(_ context.Context, cfg sdk.Config) (sdk.MachineIface, error) {
		if cfg.JailerCfg == nil || cfg.JailerCfg.CgroupVersion != "2" {
			t.Fatalf("missing cgroup-v2 jailer config: %+v", cfg.JailerCfg)
		}
		return machine, nil
	}})
	dir := t.TempDir()
	kernel := filepath.Join(dir, "vmlinux")
	rootfs := filepath.Join(dir, "rootfs")
	config := filepath.Join(dir, "vm.json")
	if err := os.WriteFile(kernel, []byte("kernel"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootfs, []byte("rootfs"), 0600); err != nil {
		t.Fatal(err)
	}
	data := `{"kernel_image_path":"` + kernel + `","rootfs":"` + rootfs + `","network_interface":"tap0","mac_address":"02:00:00:00:00:01","vcpu":1,"memory_mib":128}`
	if err := os.WriteFile(config, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.StartVM("vm", filepath.Join(dir, "vm.sock"), config); err != nil {
		t.Fatal(err)
	}
	if !machine.started || len(m.ListVMs()) != 1 {
		t.Fatalf("machine was not started: %+v", machine)
	}
	if err := m.StopVM("vm"); err != nil {
		t.Fatal(err)
	}
	if !machine.shutdown || !machine.stopped || !machine.waited || len(m.ListVMs()) != 0 {
		t.Fatalf("machine was not stopped and forgotten: %+v", machine)
	}
}
