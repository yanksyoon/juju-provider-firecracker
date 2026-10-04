# Implementation Plan: Firecracker-Juju MicroVM Provider

This document outlines the scoped, implementation-ready tasks for building a custom, production-grade Firecracker microVM provider for Juju. The architecture relies on CNI for networking, a lightweight HTTP metadata server for bootstrapping, and cgroups for process isolation.

---

## Task 1: CNI Network Wrapper Implementation

**Goal:** Create a Go module that manages Firecracker VM networking using CNI plugins instead of raw netlink.

**Implementation:**
1. Create `internal/network/cni.go`.
2. Import `github.com/containernetworking/cni/libcni` and `github.com/containernetworking/plugins/pkg/ns`.
3. Implement `SetupNetwork(containerID string) (ip string, tapName string, err error)`:
   - Load CNI config from `/etc/cni/net.d/juju-fc.conflist`.
   - Call `cni.AddNetwork()` with the containerID.
   - Parse the result to extract the allocated IP and TAP device name.
   - Return both values.
4. Implement `TeardownNetwork(containerID string) error`:
   - Call `cni.DelNetwork()` with the containerID.
   - Return `nil` (idempotent - ignore "not found" errors).
5. Create a sample CNI config file `configs/juju-fc.conflist`:
   ```json
   {
     "cniVersion": "0.4.0",
     "name": "juju-fc",
     "plugins": [
       {
         "type": "bridge",
         "bridge": "fc-br0",
         "ipam": {
           "type": "host-local",
           "subnet": "192.168.100.0/24"
         }
       }
     ]
   }
   ```

**Verification:**
- Write `internal/network/cni_test.go`.
- Test must call `SetupNetwork("test-vm-1")` and verify:
  - IP is in range `192.168.100.0/24`.
  - TAP device exists (`ip link show | grep <tapName>`).
- Test must call `TeardownNetwork("test-vm-1")` and verify:
  - TAP device is gone.
- Run test 10 times concurrently with different containerIDs to prove thread safety.
- **Pass criteria:** All tests pass, no "device exists" errors, IPs are unique.

---

## Task 2: Ultra-Fast Metadata HTTP Server

**Goal:** Build a lightweight HTTP server that serves Juju bootstrap payloads to Firecracker VMs, bypassing Cloud-Init.

**Implementation:**
1. Create `internal/metadata/server.go`.
2. Implement `MetadataServer` struct with:
   - `payloads map[string][]byte` (maps instanceID to user-data).
   - `http.Server` instance.
3. Implement `RegisterPayload(instanceID string, userData []byte)`:
   - Store the payload in the map (thread-safe with `sync.RWMutex`).
4. Implement HTTP handler for `/latest/meta-data/instance-id`:
   - Return the instanceID from the request path.
5. Implement HTTP handler for `/latest/user-data`:
   - Extract instanceID from request headers or query params.
   - Return the corresponding payload.
6. Implement `Start(port int) error` and `Stop() error` methods.
7. Add graceful shutdown with context timeout.

**Verification:**
- Write `internal/metadata/server_test.go`.
- Test must:
  - Start server on port 8080.
  - Register payload `#!/bin/bash\necho "test" > /tmp/juju-test` for instance "i-123".
  - Make HTTP GET to `http://localhost:8080/latest/user-data?instance=i-123`.
  - Verify response body matches registered payload.
  - Measure response time (must be < 10ms).
  - Stop server and verify it's no longer listening.
- **Pass criteria:** Test passes, response time < 10ms, server stops cleanly.

---

## Task 3: Firecracker Process Management with Cgroups

**Goal:** Implement direct Firecracker process lifecycle management with cgroup v2 isolation and crash-proof cleanup.

**Implementation:**
1. Create `internal/firecracker/manager.go`.
2. Implement `FirecrackerManager` struct with:
   - `processes map[string]*exec.Cmd` (tracks running VMs).
   - `cgroupBase string` (e.g., `/sys/fs/cgroup/juju-fc`).
3. Implement `StartVM(id string, socketPath string, configPath string) error`:
   - Create cgroup directory: `mkdir -p /sys/fs/cgroup/juju-fc/<id>`.
   - Execute `firecracker --api-sock <socketPath> --config-file <configPath>`.
   - Set `cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}`.
   - Write PID to cgroup: `echo <pid> > /sys/fs/cgroup/juju-fc/<id>/cgroup.procs`.
   - Store cmd in processes map.
4. Implement `StopVM(id string) error`:
   - Look up process in map.
   - Send SIGTERM, wait 5 seconds, then SIGKILL if still running.
   - Delete cgroup directory: `rmdir /sys/fs/cgroup/juju-fc/<id>`.
   - Remove from processes map.
   - Return `nil` (idempotent).
5. Implement `ListVMs() []string`:
   - Return keys from processes map.

**Verification:**
- Write `internal/firecracker/manager_test.go`.
- Test must:
  - Call `StartVM("test-vm", "/tmp/fc.sock", "/tmp/config.json")`.
  - Verify cgroup exists: `test -d /sys/fs/cgroup/juju-fc/test-vm`.
  - Verify PID is in cgroup: `cat /sys/fs/cgroup/juju-fc/test-vm/cgroup.procs`.
  - Call `StopVM("test-vm")`.
  - Verify cgroup is gone.
  - Call `StopVM("test-vm")` again (idempotency test) - must not error.
- **Pass criteria:** All tests pass, cgroup cleanup works, idempotent stop succeeds.

---

## Task 4: Minimalist Juju Provider Skeleton

**Goal:** Implement the Juju `environs.Environ` interface as a thin wrapper that orchestrates CNI, metadata server, and Firecracker manager.

**Implementation:**
1. Create `internal/provider/provider.go`.
2. Implement `FirecrackerProvider` struct with:
   - `networkMgr *network.CNIManager`
   - `metadataSrv *metadata.MetadataServer`
   - `fcMgr *firecracker.FirecrackerManager`
3. Implement required `environs.Environ` methods:
   - `StartInstance(args environs.StartInstanceParams) (*environs.StartInstanceResult, error)`:
     - Call `networkMgr.SetupNetwork(args.InstanceId)`.
     - Generate user-data payload (Juju agent bootstrap script).
     - Call `metadataSrv.RegisterPayload(args.InstanceId, userData)`.
     - Generate Firecracker config JSON (kernel, rootfs, network interface).
     - Call `fcMgr.StartVM(args.InstanceId, socketPath, configPath)`.
     - Return `&environs.StartInstanceResult{Instance: &FirecrackerInstance{...}}`.
   - `StopInstances(ids ...string) error`:
     - Loop through ids.
     - Call `fcMgr.StopVM(id)`.
     - Call `networkMgr.TeardownNetwork(id)`.
     - Return `nil` (ignore errors, idempotent).
   - `Instances(ids []instance.Id) ([]instance.Instance, error)`:
     - Return list of running instances from `fcMgr.ListVMs()`.
   - `AllInstances() ([]instance.Instance, error)`:
     - Same as above.
   - Stub out other methods (`Subnets`, `AvailabilityZones`, etc.) to return empty/nil.
4. Create `internal/provider/instance.go`:
   - Implement `FirecrackerInstance` struct with `id`, `ip`, `status`.
   - Implement `Id()`, `Status()`, `Addresses()` methods.

**Verification:**
- Write `internal/provider/provider_test.go`.
- Test must:
  - Create provider with mocked dependencies.
  - Call `StartInstance` with mock params.
  - Verify CNI `SetupNetwork` was called.
  - Verify metadata payload was registered.
  - Verify Firecracker VM was started.
  - Call `StopInstances` with the instance ID.
  - Verify Firecracker VM was stopped.
  - Verify CNI `TeardownNetwork` was called.
- **Pass criteria:** All mocks called correctly, no errors returned.

---

## Task 5: End-to-End Integration Test

**Goal:** Verify the complete flow from Juju machine request to running Firecracker VM with network connectivity.

**Implementation:**
1. Create `test/integration/e2e_test.go`.
2. Test must:
   - Initialize real CNI manager (not mocked).
   - Start real metadata server on port 8080.
   - Initialize real Firecracker manager.
   - Create provider with these real components.
   - Call `StartInstance` with:
     - `InstanceId`: "e2e-test-vm"
     - `UserData`: `#!/bin/bash\nping -c 3 8.8.8.8 > /tmp/ping-result`
   - Wait 5 seconds for VM to boot.
   - Verify:
     - TAP device exists.
     - IP is allocated.
     - Firecracker process is running.
     - Cgroup exists.
   - Call `StopInstances("e2e-test-vm")`.
   - Verify:
     - TAP device is gone.
     - IP is released.
     - Firecracker process is dead.
     - Cgroup is gone.
3. Add cleanup in `defer` block to ensure resources are freed even if test fails.

**Verification:**
- Run `go test -v ./test/integration/e2e_test.go`.
- **Pass criteria:** 
  - Test completes without timeout.
  - All verification checks pass.
  - No orphaned TAP devices (`ip link show | grep fc-` returns nothing).
  - No orphaned cgroups (`ls /sys/fs/cgroup/juju-fc` returns nothing).
  - No orphaned Firecracker processes (`ps aux | grep firecracker` returns nothing).

---

## Task 6: Documentation and CI Pipeline

**Goal:** Document the architecture and set up automated testing.

**Implementation:**
1. Create `README.md`:
   - Architecture diagram (ASCII or link to docs).
   - Prerequisites (KVM, CNI plugins, Firecracker binary).
   - Installation steps.
   - Usage example with Juju.
2. Create `docs/architecture.md`:
   - Explain the converged design decisions.
   - Document the CNI networking flow.
   - Document the metadata server protocol.
   - Document cgroup management.
3. Create `.github/workflows/ci.yml`:
   - Trigger on push and pull_request.
   - Steps:
     - Checkout code.
     - Setup Go 1.21+.
     - Install CNI plugins.
     - Run `go vet ./...`.
     - Run `golangci-lint run`.
     - Run `go test ./internal/...` (unit tests).
     - Run `go test ./test/integration/...` (e2e tests, requires KVM).
4. Create `CONTRIBUTING.md`:
   - How to run tests locally.
   - Code style guidelines.
   - PR process.

**Verification:**
- Push to GitHub.
- Check Actions tab.
- **Pass criteria:** 
  - All CI steps pass (green checkmarks).
  - README is clear and complete.
  - Architecture doc explains all design decisions.
  - CI runs both unit and integration tests.

---

## Execution Order and Dependencies

```text
Task 1 (CNI) ──┐
                ├──> Task 4 (Provider) ──> Task 5 (E2E) ──> Task 6 (Docs/CI)
Task 2 (HTTP) ──┤
                │
Task 3 (FC Mgr)─┘
```

**Recommended sequence:**
1. **Task 1** (CNI) - Foundation, no dependencies.
2. **Task 2** (HTTP) - Independent, can be done in parallel with Task 1.
3. **Task 3** (FC Manager) - Independent, can be done in parallel.
4. **Task 4** (Provider) - Depends on Tasks 1, 2, 3.
5. **Task 5** (E2E) - Depends on Task 4.
6. **Task 6** (Docs/CI) - Can be started after Task 1, finalized after Task 5.
