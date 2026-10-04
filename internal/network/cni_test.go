package network

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/containernetworking/cni/libcni"
	types "github.com/containernetworking/cni/pkg/types"
	types040 "github.com/containernetworking/cni/pkg/types/040"
)

type fakeCNI struct {
	mu        sync.Mutex
	adds      int
	dels      int
	active    int
	maxActive int
	result    types.Result
	delErr    error
}

func (f *fakeCNI) AddNetworkList(context.Context, *libcni.NetworkConfigList, *libcni.RuntimeConf) (types.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.adds++
	f.active--
	return f.result, nil
}

func (f *fakeCNI) DelNetworkList(context.Context, *libcni.NetworkConfigList, *libcni.RuntimeConf) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dels++
	return f.delErr
}

func testManager(t *testing.T, fake *fakeCNI) *CNIManager {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "juju-fc.conflist")
	if err := os.WriteFile(path, []byte(`{"cniVersion":"0.4.0","name":"juju-fc","plugins":[{"type":"bridge"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewCNIManagerWithOptions(path, CNIOptions{CNI: fake})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestSetupAndTeardownExtractsIPAndInterface(t *testing.T) {
	fake := &fakeCNI{result: &types040.Result{
		CNIVersion: "0.4.0",
		Interfaces: []*types040.Interface{{Name: "tap-test"}},
		IPs:        []*types040.IPConfig{{Address: net.IPNet{IP: net.ParseIP("192.168.100.23"), Mask: net.CIDRMask(24, 32)}}},
	}}
	manager := testManager(t, fake)

	ip, tap, err := manager.SetupNetwork("vm-1")
	if err != nil || ip != "192.168.100.23" || tap != "tap-test" {
		t.Fatalf("SetupNetwork() = %q, %q, %v", ip, tap, err)
	}
	if err := manager.TeardownNetwork("vm-1"); err != nil {
		t.Fatalf("TeardownNetwork() error = %v", err)
	}
	if fake.adds != 1 || fake.dels != 1 {
		t.Fatalf("CNI calls = add %d, del %d; want 1, 1", fake.adds, fake.dels)
	}
}

func TestNetworkOperationsAreSerialized(t *testing.T) {
	fake := &fakeCNI{result: &types040.Result{
		CNIVersion: "0.4.0",
		Interfaces: []*types040.Interface{{Name: "tap-test"}},
		IPs:        []*types040.IPConfig{{Address: net.IPNet{IP: net.ParseIP("192.168.100.23"), Mask: net.CIDRMask(24, 32)}}},
	}}
	manager := testManager(t, fake)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = manager.SetupNetwork("vm")
		}()
	}
	wg.Wait()
	if fake.maxActive != 1 {
		t.Fatalf("concurrent CNI calls = %d; want 1", fake.maxActive)
	}
}

func TestTeardownIgnoresMissingResources(t *testing.T) {
	fake := &fakeCNI{delErr: errors.New("network not found")}
	manager := testManager(t, fake)
	if err := manager.TeardownNetwork("vm-1"); err != nil {
		t.Fatalf("TeardownNetwork() error = %v", err)
	}
}

func TestSetupCleansUpMalformedResult(t *testing.T) {
	fake := &fakeCNI{result: &types040.Result{CNIVersion: "0.4.0"}}
	manager := testManager(t, fake)
	if _, _, err := manager.SetupNetwork("vm-1"); err == nil {
		t.Fatal("SetupNetwork() succeeded with an incomplete CNI result")
	}
	if fake.dels != 1 {
		t.Fatalf("cleanup DEL calls = %d; want 1", fake.dels)
	}
}
