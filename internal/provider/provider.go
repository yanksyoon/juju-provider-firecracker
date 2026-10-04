// Package provider adapts the Firecracker lifecycle to Juju's instance broker.
package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/canonical/juju-provider-firecracker/internal/firecracker"
	"github.com/canonical/juju-provider-firecracker/internal/metadata"
	"github.com/canonical/juju-provider-firecracker/internal/network"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/environs"
	jujucontext "github.com/juju/juju/environs/context"
	"github.com/juju/juju/environs/instances"
)

// Network is the portion of CNI used by the provider.
type Network interface {
	SetupNetwork(string) (string, string, error)
	TeardownNetwork(string) error
}

// Metadata is the portion of the metadata server used by the provider.
type Metadata interface{ RegisterPayload(string, []byte) }

// VMManager is the portion of Firecracker used by the provider.
type VMManager interface {
	StartVM(string, string, string) error
	StopVM(string) error
	ListVMs() []string
}

// Options supplies dependencies and host paths. The default constructor uses
// the production CNI, metadata, and Firecracker implementations.
type Options struct {
	Network    Network
	Metadata   Metadata
	VM         VMManager
	ConfigDir  string
	SocketDir  string
	KernelPath string
	RootFSPath string
}

// FirecrackerProvider is intentionally a thin orchestration layer. It owns no
// Juju credentials and performs no calls to a live controller.
type FirecrackerProvider struct {
	network                                      Network
	metadata                                     Metadata
	vm                                           VMManager
	configDir, socketDir, kernelPath, rootFSPath string
	mu                                           sync.RWMutex
	instances                                    map[instance.Id]*FirecrackerInstance
}

func New(opts Options) (*FirecrackerProvider, error) {
	if opts.Network == nil {
		mgr, err := network.NewCNIManager("")
		if err != nil {
			return nil, err
		}
		opts.Network = mgr
	}
	if opts.Metadata == nil {
		opts.Metadata = metadata.NewMetadataServer()
	}
	if opts.VM == nil {
		opts.VM = firecracker.NewFirecrackerManager("/sys/fs/cgroup/juju-fc")
	}
	if opts.ConfigDir == "" {
		opts.ConfigDir = filepath.Join(os.TempDir(), "juju-firecracker")
	}
	if opts.SocketDir == "" {
		opts.SocketDir = opts.ConfigDir
	}
	return &FirecrackerProvider{network: opts.Network, metadata: opts.Metadata, vm: opts.VM,
		configDir: opts.ConfigDir, socketDir: opts.SocketDir, kernelPath: opts.KernelPath,
		rootFSPath: opts.RootFSPath, instances: make(map[instance.Id]*FirecrackerInstance)}, nil
}

var _ environs.InstanceBroker = (*FirecrackerProvider)(nil)

// StartInstance allocates network state, publishes bootstrap data, writes a
// Firecracker config, and starts the VM. Every failure after CNI setup rolls
// the network attachment back.
func (p *FirecrackerProvider) StartInstance(_ jujucontext.ProviderCallContext, args environs.StartInstanceParams) (*environs.StartInstanceResult, error) {
	if args.InstanceConfig == nil || args.InstanceConfig.MachineId == "" {
		return nil, errors.New("instance config with machine ID is required")
	}
	id := instance.Id(args.InstanceConfig.MachineId)
	ip, tap, err := p.network.SetupNetwork(string(id))
	if err != nil {
		return nil, fmt.Errorf("setup network for %q: %w", id, err)
	}
	rollback := true
	defer func() {
		if rollback {
			_ = p.network.TeardownNetwork(string(id))
		}
	}()
	payload := []byte(fmt.Sprintf("#!/bin/sh\n# Juju machine %s\n", id))
	p.metadata.RegisterPayload(string(id), payload)
	configPath, socketPath, err := p.writeConfig(id, ip, tap)
	if err != nil {
		return nil, err
	}
	if err := p.vm.StartVM(string(id), socketPath, configPath); err != nil {
		return nil, fmt.Errorf("start VM %q: %w", id, err)
	}
	inst := &FirecrackerInstance{id: id, ip: ip, status: "running"}
	p.mu.Lock()
	p.instances[id] = inst
	p.mu.Unlock()
	rollback = false
	return &environs.StartInstanceResult{Instance: inst}, nil
}

func (p *FirecrackerProvider) writeConfig(id instance.Id, ip, tap string) (string, string, error) {
	if err := os.MkdirAll(p.configDir, 0700); err != nil {
		return "", "", fmt.Errorf("create config directory: %w", err)
	}
	if err := os.MkdirAll(p.socketDir, 0700); err != nil {
		return "", "", fmt.Errorf("create socket directory: %w", err)
	}
	configPath := filepath.Join(p.configDir, string(id)+".json")
	socketPath := filepath.Join(p.socketDir, string(id)+".sock")
	cfg := struct {
		KernelImagePath  string `json:"kernel_image_path"`
		RootFS           string `json:"rootfs"`
		NetworkInterface string `json:"network_interface"`
		IP               string `json:"ip"`
	}{p.kernelPath, p.rootFSPath, tap, ip}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return "", "", fmt.Errorf("write VM config: %w", err)
	}
	return configPath, socketPath, nil
}

// StopInstances is idempotent and attempts both VM and network cleanup for
// every requested ID, returning the first operational error after cleanup.
func (p *FirecrackerProvider) StopInstances(_ jujucontext.ProviderCallContext, ids ...instance.Id) error {
	var first error
	for _, id := range ids {
		p.mu.RLock()
		_, known := p.instances[id]
		p.mu.RUnlock()
		if !known {
			// Juju requires unknown IDs to be ignored so callers can safely
			// retry a stop operation.
			continue
		}
		vmErr := p.vm.StopVM(string(id))
		networkErr := p.network.TeardownNetwork(string(id))
		if vmErr != nil && first == nil {
			first = vmErr
		}
		if networkErr != nil && first == nil {
			first = networkErr
		}
		p.mu.Lock()
		if vmErr == nil && networkErr == nil {
			delete(p.instances, id)
		}
		p.mu.Unlock()
	}
	return first
}

func (p *FirecrackerProvider) Instances(_ jujucontext.ProviderCallContext, ids []instance.Id) ([]instances.Instance, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]instances.Instance, 0, len(ids))
	for _, id := range ids {
		if inst, ok := p.instances[id]; ok {
			result = append(result, inst)
		}
	}
	return result, nil
}

func (p *FirecrackerProvider) AllInstances(_ jujucontext.ProviderCallContext) ([]instances.Instance, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]instances.Instance, 0, len(p.instances))
	for _, id := range p.vm.ListVMs() {
		if inst, ok := p.instances[instance.Id(id)]; ok {
			result = append(result, inst)
		}
	}
	return result, nil
}

func (p *FirecrackerProvider) AllRunningInstances(ctx jujucontext.ProviderCallContext) ([]instances.Instance, error) {
	return p.AllInstances(ctx)
}

// The remaining Environ capabilities are deliberately not advertised by this
// skeleton. Juju's full environs.Environ also requires bootstrap, storage,
// image, and provider configuration APIs; those need real product decisions.
