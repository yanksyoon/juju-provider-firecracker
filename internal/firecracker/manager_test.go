package firecracker

import (
	"context"
	"net"
	"testing"

	sdk "github.com/firecracker-microvm/firecracker-go-sdk"
)

type fakeMachine struct {
	sdk.MachineIface
	started, shutdown, stopped bool
}

func (m *fakeMachine) Start(context.Context) error    { m.started = true; return nil }
func (m *fakeMachine) Shutdown(context.Context) error { m.shutdown = true; return nil }
func (m *fakeMachine) StopVMM() error                 { m.stopped = true; return nil }
func (m *fakeMachine) Wait(context.Context) error     { return nil }

func validRequest(t *testing.T) VMRequest {
	t.Helper()
	_, n, err := net.ParseCIDR("192.0.2.10/24")
	if err != nil {
		t.Fatal(err)
	}
	return VMRequest{ID: "vm", SocketPath: "/tmp/vm.sock", KernelPath: "/boot/vmlinux", RootFSPath: "/var/lib/vm.ext4", TapName: "tap0", MACAddress: "02:00:00:00:00:01", IP: *n, VCPU: 2, MemoryMiB: 1024, KernelArgs: "console=ttyS0"}
}

func TestSDKConfigMapsTypedRequest(t *testing.T) {
	cfg, err := SDKConfig(validRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VMID != "vm" || cfg.SocketPath != "/tmp/vm.sock" || cfg.KernelImagePath != "/boot/vmlinux" || cfg.KernelArgs != "console=ttyS0" {
		t.Fatalf("unexpected SDK config: %+v", cfg)
	}
	if len(cfg.Drives) != 1 || sdk.StringValue(cfg.Drives[0].PathOnHost) != "/var/lib/vm.ext4" || !sdk.BoolValue(cfg.Drives[0].IsRootDevice) {
		t.Fatalf("unexpected drives: %+v", cfg.Drives)
	}
	if sdk.Int64Value(cfg.MachineCfg.VcpuCount) != 2 || sdk.Int64Value(cfg.MachineCfg.MemSizeMib) != 1024 {
		t.Fatalf("unexpected sizing: %+v", cfg.MachineCfg)
	}
	if cfg.NetworkInterfaces[0].StaticConfiguration.HostDevName != "tap0" {
		t.Fatalf("unexpected network: %+v", cfg.NetworkInterfaces)
	}
}

func TestSDKStartStopUsesMachine(t *testing.T) {
	machine := &fakeMachine{}
	var received sdk.Config
	m := NewFirecrackerManagerWithOptions(t.TempDir(), Options{NewMachine: func(_ context.Context, cfg sdk.Config) (sdk.MachineIface, error) { received = cfg; return machine, nil }})
	request := validRequest(t)
	if err := m.StartVM(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if received.JailerCfg == nil || received.JailerCfg.CgroupVersion != "2" {
		t.Fatalf("missing cgroup-v2 jailer config: %+v", received.JailerCfg)
	}
	if len(m.ListVMs()) != 1 || !machine.started {
		t.Fatalf("machine was not started")
	}
	if err := m.StopVM(request.ID); err != nil {
		t.Fatal(err)
	}
	if !machine.shutdown || !machine.stopped || len(m.ListVMs()) != 0 {
		t.Fatalf("machine was not stopped: %+v", machine)
	}
}

func TestStartFailureDoesNotPublishMachine(t *testing.T) {
	m := NewFirecrackerManagerWithOptions(t.TempDir(), Options{NewMachine: func(context.Context, sdk.Config) (sdk.MachineIface, error) { return nil, context.Canceled }})
	if err := m.StartVM(context.Background(), validRequest(t)); err == nil {
		t.Fatal("expected machine creation error")
	}
	if len(m.ListVMs()) != 0 {
		t.Fatal("failed start was published")
	}
}
