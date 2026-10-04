// Package integration contains tests that exercise host resources only after
// explicit prerequisite and environment gates.
package integration

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/canonical/juju-provider-firecracker/internal/firecracker"
	"github.com/canonical/juju-provider-firecracker/internal/metadata"
	"github.com/canonical/juju-provider-firecracker/internal/network"
	"github.com/canonical/juju-provider-firecracker/internal/provider"
	"github.com/juju/juju/cloudconfig/instancecfg"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/environs"
)

type prerequisites struct {
	firecracker string
	cniConfig   string
	cniPath     string
	cgroupBase  string
	kernel      string
	rootFS      string
}

const e2eUserData = "#!/bin/bash\nping -c 3 8.8.8.8 > /tmp/ping-result\n"

// checkPrerequisites is deliberately side-effect free. It only discovers
// capabilities; the test decides whether it is safe to allocate resources.
func checkPrerequisites() (prerequisites, []string) {
	var p prerequisites
	var missing []string

	if runtime.GOOS != "linux" {
		missing = append(missing, "Linux is required")
	} else if _, err := os.Stat("/dev/kvm"); err != nil {
		missing = append(missing, fmt.Sprintf("KVM (/dev/kvm): %v", err))
	}

	p.firecracker = os.Getenv("FIRECRACKER_BIN")
	if p.firecracker == "" {
		p.firecracker, _ = exec.LookPath("firecracker")
	}
	if p.firecracker == "" {
		missing = append(missing, "Firecracker binary (set FIRECRACKER_BIN or put firecracker on PATH)")
	} else if info, err := os.Stat(p.firecracker); err != nil || info.IsDir() {
		missing = append(missing, fmt.Sprintf("usable Firecracker binary %q", p.firecracker))
	}

	p.cniConfig = os.Getenv("JUJU_FC_E2E_CNI_CONFIG")
	if p.cniConfig == "" {
		p.cniConfig = os.Getenv("JUJU_FC_CNI_CONFIG")
	}
	if p.cniConfig == "" {
		p.cniConfig = "/etc/cni/net.d/juju-fc.conflist"
	}
	if info, err := os.Stat(p.cniConfig); err != nil || info.IsDir() {
		missing = append(missing, fmt.Sprintf("CNI config %q", p.cniConfig))
	}

	p.cniPath = os.Getenv("CNI_PATH")
	if p.cniPath == "" {
		p.cniPath = "/opt/cni/bin"
	}
	if !hasCNIPlugin(p.cniPath) {
		missing = append(missing, fmt.Sprintf("CNI plugins in %q (bridge and host-local required)", p.cniPath))
	}

	p.cgroupBase = os.Getenv("JUJU_FC_CGROUP_BASE")
	if p.cgroupBase == "" {
		p.cgroupBase = "/sys/fs/cgroup/juju-fc"
	}
	if info, err := os.Stat(p.cgroupBase); err != nil || !info.IsDir() {
		missing = append(missing, fmt.Sprintf("cgroup v2 base directory %q", p.cgroupBase))
	} else {
		probe := filepath.Join(p.cgroupBase, ".juju-fc-integration-probe")
		if err := os.Mkdir(probe, 0700); err != nil {
			missing = append(missing, fmt.Sprintf("write permission for cgroup base %q: %v", p.cgroupBase, err))
		} else if err := os.Remove(probe); err != nil {
			missing = append(missing, fmt.Sprintf("cleanup permission for cgroup base %q: %v", p.cgroupBase, err))
		}
	}
	p.kernel = os.Getenv("JUJU_FC_E2E_KERNEL")
	if p.kernel == "" {
		missing = append(missing, "JUJU_FC_E2E_KERNEL (disposable guest kernel)")
	} else if info, err := os.Stat(p.kernel); err != nil || info.IsDir() {
		missing = append(missing, fmt.Sprintf("usable guest kernel %q", p.kernel))
	}
	p.rootFS = os.Getenv("JUJU_FC_E2E_ROOTFS")
	if p.rootFS == "" {
		missing = append(missing, "JUJU_FC_E2E_ROOTFS (disposable guest rootfs)")
	} else if info, err := os.Stat(p.rootFS); err != nil || info.IsDir() {
		missing = append(missing, fmt.Sprintf("usable guest rootfs %q", p.rootFS))
	}
	return p, missing
}

func hasCNIPlugin(path string) bool {
	for _, name := range []string{"bridge", "host-local"} {
		found := false
		for _, dir := range strings.Split(path, string(os.PathListSeparator)) {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func disposableID() string {
	return fmt.Sprintf("juju-fc-e2e-%d", time.Now().UnixNano())
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate ephemeral localhost port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// TestPrerequisiteChecks exercises the non-privileged discovery path. It must
// never create a VM, alter CNI state, contact Juju, or require credentials.
func TestPrerequisiteChecks(t *testing.T) {
	p, missing := checkPrerequisites()
	if p.cniConfig == "" || p.cniPath == "" || p.cgroupBase == "" {
		t.Fatal("prerequisite discovery returned empty defaults")
	}
	t.Logf("discovered prerequisites: %+v", p)
	if len(missing) > 0 {
		t.Logf("integration prerequisites unavailable (expected on ordinary CI): %s", strings.Join(missing, "; "))
	}
}

// TestEndToEnd is opt-in even on a capable host. Set JUJU_FC_RUN_E2E=1 only in
// an isolated test environment with disposable CNI/cgroup configuration.
func TestEndToEnd(t *testing.T) {
	p, missing := checkPrerequisites()
	if len(missing) > 0 {
		t.Skipf("integration prerequisites unavailable: %s", strings.Join(missing, "; "))
	}
	if os.Getenv("JUJU_FC_RUN_E2E") != "1" {
		t.Skip("set JUJU_FC_RUN_E2E=1 to run the destructive integration path in an isolated environment")
	}
	if os.Getenv("JUJU_FC_E2E_CNI_CONFIG") == "" || os.Getenv("JUJU_FC_CGROUP_BASE") == "" {
		t.Fatal("JUJU_FC_RUN_E2E=1 requires explicit JUJU_FC_E2E_CNI_CONFIG and JUJU_FC_CGROUP_BASE; refusing host defaults")
	}

	id := disposableID()
	// The test process itself is bounded by go test; provider calls are
	// synchronous and do not use a live Juju controller context.

	// Keep every resource under this run's disposable identity. The actual
	// provider lifecycle is supplied by the package under test in deployments;
	// this guard prevents accidental execution against a production Juju model.
	if strings.Contains(strings.ToLower(os.Getenv("JUJU_MODEL")), "prod") || os.Getenv("JUJU_CONTROLLER") != "" {
		t.Fatal("refusing integration test with JUJU_MODEL/ JUJU_CONTROLLER configured")
	}
	if strings.Contains(strings.ToLower(p.cniConfig), "/etc/cni/net.d") {
		t.Fatal("refusing integration test with a system CNI config; use a disposable config")
	}

	netMgr, err := network.NewCNIManager(p.cniConfig)
	if err != nil {
		t.Fatalf("create real CNI manager: %v", err)
	}
	meta := metadata.NewMetadataServer()
	if err := meta.Start(0); err != nil {
		t.Fatalf("start metadata server: %v", err)
	}
	metadataAddr := meta.Addr()
	defer func() {
		if err := meta.Stop(); err != nil {
			t.Errorf("stop metadata server: %v", err)
		}
		if meta.Addr() != "" {
			t.Errorf("metadata server still has an address after Stop: %s", meta.Addr())
		}
	}()
	vmMgr := firecracker.NewFirecrackerManager(p.cgroupBase)
	workDir := t.TempDir()
	prov, err := provider.New(provider.Options{
		Network: netMgr, Metadata: meta, VM: vmMgr,
		ConfigDir: workDir, SocketDir: workDir,
		KernelPath: p.kernel, RootFSPath: p.rootFS,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	started := false
	defer func() {
		if started {
			if err := prov.StopInstances(nil, instance.Id(id)); err != nil {
				t.Errorf("deferred provider cleanup for %q: %v", id, err)
			}
		}
		if err := os.RemoveAll(workDir); err != nil {
			t.Errorf("remove disposable work directory: %v", err)
		}
		if _, err := os.Stat(filepath.Join(p.cgroupBase, id)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("cgroup remains after cleanup: %v", err)
		}
	}()

	result, err := prov.StartInstance(nil, environs.StartInstanceParams{InstanceConfig: &instancecfg.InstanceConfig{
		MachineId: id,
		// Juju 3.6 carries caller-provided guest data through this field;
		// the provider's current bootstrap generator remains authoritative.
		CloudInitUserData: map[string]interface{}{"user-data": e2eUserData},
	}})
	if err != nil {
		t.Fatalf("start disposable instance: %v", err)
	}
	started = true
	if result == nil || result.Instance == nil || result.Instance.Id() != instance.Id(id) {
		t.Fatalf("unexpected started instance: %#v", result)
	}

	// The provider registers the payload before launching the VM. Verify that
	// the real HTTP server serves it without printing its contents in logs.
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + meta.Addr() + "/latest/user-data?instance=" + id)
	if err != nil {
		t.Fatalf("fetch metadata payload: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), id) {
		t.Fatalf("metadata response status=%d read=%v close=%v payload contains ID=%t", response.StatusCode, readErr, closeErr, strings.Contains(string(body), id))
	}

	cgroup := filepath.Join(p.cgroupBase, id)
	pidPath := filepath.Join(cgroup, "cgroup.procs")
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read VM cgroup membership: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid Firecracker PID %q: %v", strings.TrimSpace(string(pidData)), err)
	}
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); err != nil {
		t.Fatalf("Firecracker process %d is not present: %v", pid, err)
	}
	addresses, err := result.Instance.Addresses(nil)
	if err != nil || len(addresses) == 0 {
		t.Fatalf("started instance has no address: %v", err)
	}
	if !interfaceHasIP(addresses[0].Value) {
		t.Fatalf("allocated IP %q is not assigned to a host interface", addresses[0].Value)
	}
	if len(vmMgr.ListVMs()) != 1 || vmMgr.ListVMs()[0] != id {
		t.Fatalf("tracked VMs = %v", vmMgr.ListVMs())
	}

	if err := prov.StopInstances(nil, instance.Id(id)); err != nil {
		t.Fatalf("stop disposable instance: %v", err)
	}
	started = false
	if _, err := os.Stat(cgroup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cgroup still exists after stop: %v", err)
	}
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); err == nil {
		t.Fatalf("Firecracker process %d remains after stop", pid)
	}
	if interfaceHasIP(addresses[0].Value) {
		t.Fatalf("allocated IP %q remains assigned after network cleanup", addresses[0].Value)
	}
	if len(vmMgr.ListVMs()) != 0 {
		t.Fatalf("tracked VMs after stop = %v", vmMgr.ListVMs())
	}
	if err := meta.Stop(); err != nil {
		t.Fatalf("stop metadata server: %v", err)
	}
	if meta.Addr() != "" {
		t.Fatalf("metadata server still has an address after Stop: %s", meta.Addr())
	}
	if _, err := client.Get("http://" + metadataAddr + "/latest/user-data?instance=" + id); err == nil {
		t.Fatal("metadata endpoint remained reachable after Stop")
	}
	if err := os.RemoveAll(workDir); err != nil {
		t.Fatalf("remove disposable work directory: %v", err)
	}
	if _, err := os.Stat(workDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disposable work directory remains after cleanup: %v", err)
	}
}

func interfaceHasIP(want string) bool {
	interfaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if strings.Split(addr.String(), "/")[0] == want {
				return true
			}
		}
	}
	return false
}

// TestEndToEndCleanup documents the invariant used by the real lifecycle: all
// resources must be released even when assertions fail. It is kept independent
// of host privileges so cleanup regressions can be tested in ordinary CI.
func TestEndToEndCleanup(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "resource")
	if err := os.WriteFile(marker, []byte("allocated"), 0600); err != nil {
		t.Fatal(err)
	}
	cleanup := func() error { return os.Remove(marker) }
	defer func() {
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("deferred cleanup did not remove disposable resource: %v", err)
		}
	}()
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
}
