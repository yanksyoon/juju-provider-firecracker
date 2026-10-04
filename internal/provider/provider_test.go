package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/canonical/juju-provider-firecracker/internal/firecracker"
	"github.com/juju/juju/cloudconfig/instancecfg"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/environs"
)

type fakeNetwork struct {
	setup, teardown []string
	ip, tap         string
	setupErr        error
}

func (f *fakeNetwork) SetupNetwork(id string) (string, string, error) {
	f.setup = append(f.setup, id)
	return f.ip, f.tap, f.setupErr
}
func (f *fakeNetwork) TeardownNetwork(id string) error {
	f.teardown = append(f.teardown, id)
	return nil
}

type fakeMetadata struct {
	id   string
	data []byte
}

func (f *fakeMetadata) RegisterPayload(id string, data []byte) {
	f.id, f.data = id, append([]byte(nil), data...)
}

type fakeVM struct {
	started, stopped []string
	ids              []string
	startErr         error
	stopErr          error
	request          firecracker.VMRequest
}

func (f *fakeVM) StartVM(_ context.Context, request firecracker.VMRequest) error {
	f.request = request
	f.started = append(f.started, request.ID)
	return f.startErr
}
func (f *fakeVM) StopVM(id string) error { f.stopped = append(f.stopped, id); return f.stopErr }
func (f *fakeVM) ListVMs() []string      { return append([]string(nil), f.ids...) }

func testConfig() Config {
	return Config{
		KernelImagePath: "/tmp/kernel",
		RootFSPath:      "/tmp/rootfs",
		CgroupBase:      "/test/cgroup",
		CNIConfigPath:   "/test/cni.conflist",
		StopTimeout:     5e9, // 5s as ns
		ShutdownTimeout: 5e9,
	}
}

func TestLifecycleOrchestratesDependenciesAndRollsBack(t *testing.T) {
	net := &fakeNetwork{ip: "192.168.100.5", tap: "tap0"}
	meta := &fakeMetadata{}
	vm := &fakeVM{}
	cfg := testConfig()
	cfg.ConfigDir = t.TempDir()
	cfg.SocketDir = t.TempDir()
	p, err := New(Options{Config: cfg, Network: net, Metadata: meta, VM: vm})
	if err != nil {
		t.Fatal(err)
	}
	args := environs.StartInstanceParams{InstanceConfig: &instancecfg.InstanceConfig{MachineId: "machine-1"}}
	result, err := p.StartInstance(nil, args)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Instance.Id(); string(got) != "machine-1" {
		t.Fatalf("instance ID = %q", got)
	}
	if len(net.setup) != 1 || net.setup[0] != "machine-1" || meta.id != "machine-1" || len(meta.data) == 0 || len(vm.started) != 1 {
		t.Fatalf("unexpected calls: setup=%v metadata=%q start=%v", net.setup, meta.id, vm.started)
	}
	if vm.request.ID != "machine-1" || vm.request.KernelPath != cfg.KernelImagePath || vm.request.RootFSPath != cfg.RootFSPath || vm.request.TapName != "tap0" || vm.request.VCPU != 1 || vm.request.MemoryMiB != 512 {
		t.Fatalf("manager did not receive typed SDK request: %+v", vm.request)
	}
	if err := p.StopInstances(nil, "machine-1"); err != nil {
		t.Fatal(err)
	}
	if len(vm.stopped) != 1 || len(net.teardown) != 1 {
		t.Fatalf("cleanup calls: stop=%v teardown=%v", vm.stopped, net.teardown)
	}
}

func TestStartRollsBackNetworkWhenVMFails(t *testing.T) {
	net := &fakeNetwork{ip: "192.168.100.6", tap: "tap1"}
	vm := &fakeVM{startErr: errors.New("boom")}
	cfg := testConfig()
	cfg.ConfigDir = t.TempDir()
	cfg.SocketDir = t.TempDir()
	p, err := New(Options{Config: cfg, Network: net, Metadata: &fakeMetadata{}, VM: vm})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.StartInstance(nil, environs.StartInstanceParams{InstanceConfig: &instancecfg.InstanceConfig{MachineId: "machine-2"}})
	if err == nil || len(net.teardown) != 1 || net.teardown[0] != "machine-2" {
		t.Fatalf("err=%v teardown=%v", err, net.teardown)
	}
}

func TestInstancesListingAndUnknownStop(t *testing.T) {
	net := &fakeNetwork{ip: "192.168.100.7", tap: "tap2"}
	vm := &fakeVM{ids: []string{"machine-3"}, stopErr: errors.New("unexpected stop")}
	cfg := testConfig()
	cfg.ConfigDir = t.TempDir()
	cfg.SocketDir = t.TempDir()
	p, err := New(Options{Config: cfg, Network: net, Metadata: &fakeMetadata{}, VM: vm})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.StartInstance(nil, environs.StartInstanceParams{InstanceConfig: &instancecfg.InstanceConfig{MachineId: "machine-3"}})
	if err != nil {
		t.Fatal(err)
	}
	all, err := p.AllInstances(nil)
	if err != nil || len(all) != 1 || all[0].Id() != "machine-3" {
		t.Fatalf("all instances = %v, err = %v", all, err)
	}
	running, err := p.AllRunningInstances(nil)
	if err != nil || len(running) != 1 || running[0].Id() != "machine-3" {
		t.Fatalf("running instances = %v, err = %v", running, err)
	}
	selected, err := p.Instances(nil, []instance.Id{"missing", "machine-3"})
	if err != nil || len(selected) != 1 || selected[0].Id() != "machine-3" {
		t.Fatalf("selected instances = %v, err = %v", selected, err)
	}
	// Unknown IDs must be ignored without reaching the VM or CNI boundary.
	if err := p.StopInstances(nil, "missing"); err != nil || len(vm.stopped) != 0 || len(net.teardown) != 0 {
		t.Fatalf("unknown stop: err=%v stopped=%v teardown=%v", err, vm.stopped, net.teardown)
	}
	// A failed cleanup remains tracked so a subsequent stop can retry it.
	if err := p.StopInstances(nil, "machine-3"); err == nil {
		t.Fatal("expected stop error")
	}
	vm.stopErr = nil
	if err := p.StopInstances(nil, "machine-3"); err != nil {
		t.Fatal(err)
	}
	if err := p.StopInstances(nil, "machine-3"); err != nil || len(vm.stopped) != 2 || len(net.teardown) != 2 {
		t.Fatalf("repeated stop: err=%v stopped=%v teardown=%v", err, vm.stopped, net.teardown)
	}
}
