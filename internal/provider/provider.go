// Package provider adapts the Firecracker lifecycle to Juju's instance broker.
package provider

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/canonical/juju-provider-firecracker/internal/firecracker"
	"github.com/canonical/juju-provider-firecracker/internal/metadata"
	"github.com/canonical/juju-provider-firecracker/internal/network"
	"github.com/juju/errors"
	"github.com/juju/juju/cloudconfig/instancecfg"
	"github.com/juju/juju/core/constraints"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	jujucontext "github.com/juju/juju/environs/context"
	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/storage"
	"github.com/juju/version/v2"
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

// Options supplies dependencies for the provider. Production callers use
// New() with only Config populated; test callers supply fakes for Network,
// Metadata, and VM.
type Options struct {
	Config   Config
	Network  Network
	Metadata Metadata
	VM       VMManager
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
	cfg := opts.Config

	if opts.Network == nil {
		mgr, err := network.NewCNIManagerWithOptions(cfg.CNIConfigPath, network.CNIOptions{})
		if err != nil {
			return nil, err
		}
		opts.Network = mgr
	}
	if opts.Metadata == nil {
		opts.Metadata = metadata.NewMetadataServer()
	}
	if opts.VM == nil {
		opts.VM = firecracker.NewFirecrackerManagerWithOptions(cfg.CgroupBase, firecracker.Options{
			Command:     cfg.FirecrackerBinary,
			StopTimeout: cfg.StopTimeout,
		})
	}

	if cfg.ConfigDir == "" {
		cfg.ConfigDir = filepath.Join(os.TempDir(), "juju-firecracker")
	}
	if cfg.SocketDir == "" {
		cfg.SocketDir = cfg.ConfigDir
	}

	return &FirecrackerProvider{network: opts.Network, metadata: opts.Metadata, vm: opts.VM,
		configDir: cfg.ConfigDir, socketDir: cfg.SocketDir, kernelPath: cfg.KernelImagePath,
		rootFSPath: cfg.RootFSPath, instances: make(map[instance.Id]*FirecrackerInstance)}, nil
}

var _ environs.InstanceBroker = (*FirecrackerProvider)(nil)

// StartInstance allocates network state, publishes bootstrap data, writes a
// Firecracker config, and starts the VM. Every failure after CNI setup rolls
// the network attachment back.
func (p *FirecrackerProvider) StartInstance(_ jujucontext.ProviderCallContext, args environs.StartInstanceParams) (*environs.StartInstanceResult, error) {
	if args.InstanceConfig == nil || args.InstanceConfig.MachineId == "" {
		return nil, stderrors.New("instance config with machine ID is required")
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

// ---------- environs.Environ implementation ----------

// environ wraps FirecrackerProvider to satisfy environs.Environ.
type environ struct {
	*FirecrackerProvider
	cfg       *config.Config
	ecfgMutex sync.RWMutex
	provider  environs.EnvironProvider
}

func newEnviron(jujuCfg *config.Config, fcCfg Config) (*environ, error) {
	prov, err := New(Options{Config: fcCfg})
	if err != nil {
		return nil, err
	}
	return &environ{
		FirecrackerProvider: prov,
		cfg:                 jujuCfg,
		provider:            &environProvider{},
	}, nil
}

func (e *environ) Config() *config.Config {
	e.ecfgMutex.RLock()
	defer e.ecfgMutex.RUnlock()
	return e.cfg
}

func (e *environ) SetConfig(cfg *config.Config) error {
	e.ecfgMutex.Lock()
	defer e.ecfgMutex.Unlock()
	e.cfg = cfg
	return nil
}

func (e *environ) Provider() environs.EnvironProvider { return e.provider }

func (e *environ) PrepareForBootstrap(_ environs.BootstrapContext, _ string) error {
	return nil
}

func (e *environ) Bootstrap(ctx environs.BootstrapContext, callCtx jujucontext.ProviderCallContext, params environs.BootstrapParams) (*environs.BootstrapResult, error) {
	_, err := e.StartInstance(callCtx, environs.StartInstanceParams{
		ControllerUUID: params.ControllerConfig.ControllerUUID(),
		Constraints:    params.BootstrapConstraints,
		ImageMetadata:  params.ImageMetadata,
		Tools:          params.AvailableTools,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap start instance: %w", err)
	}
	return &environs.BootstrapResult{
		Arch: "amd64",
		Base: params.BootstrapBase,
		CloudBootstrapFinalizer: func(_ environs.BootstrapContext, _ *instancecfg.InstanceConfig, _ environs.BootstrapDialOpts) error {
			return nil
		},
	}, nil
}

func (e *environ) Create(_ jujucontext.ProviderCallContext, _ environs.CreateParams) error {
	return nil
}

func (e *environ) Destroy(_ jujucontext.ProviderCallContext) error {
	ids := e.vm.ListVMs()
	var firstErr error
	for _, id := range ids {
		if err := e.StopInstances(nil, instance.Id(id)); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("stop instance %q during destroy: %w", id, err)
		}
	}
	return firstErr
}

func (e *environ) DestroyController(_ jujucontext.ProviderCallContext, _ string) error {
	return e.Destroy(nil)
}

func (e *environ) ControllerInstances(_ jujucontext.ProviderCallContext, _ string) ([]instance.Id, error) {
	ids := e.vm.ListVMs()
	result := make([]instance.Id, len(ids))
	for i, id := range ids {
		result[i] = instance.Id(id)
	}
	if len(result) == 0 {
		return nil, environs.ErrNoInstances
	}
	return result, nil
}

func (e *environ) ConstraintsValidator(_ jujucontext.ProviderCallContext) (constraints.Validator, error) {
	v := constraints.NewValidator()
	v.RegisterUnsupported([]string{
		constraints.CpuPower,
		constraints.VirtType,
		constraints.Cores,
	})
	v.RegisterConflicts([]string{constraints.InstanceType}, []string{constraints.Mem})
	return v, nil
}

func (e *environ) PrecheckInstance(_ jujucontext.ProviderCallContext, _ environs.PrecheckInstanceParams) error {
	return nil
}

func (e *environ) InstanceTypes(_ jujucontext.ProviderCallContext, _ constraints.Value) (instances.InstanceTypesWithCostMetadata, error) {
	return instances.InstanceTypesWithCostMetadata{}, nil
}

func (e *environ) AdoptResources(_ jujucontext.ProviderCallContext, _ string, _ version.Number) error {
	return nil
}

// StorageProviderTypes returns no storage providers — Firecracker is ephemeral.
func (e *environ) StorageProviderTypes() ([]storage.ProviderType, error) {
	return nil, nil
}

// StorageProvider returns an error — no storage providers are registered.
func (e *environ) StorageProvider(t storage.ProviderType) (storage.Provider, error) {
	return nil, errors.NotFoundf("storage provider %q", t)
}

// Ensure compile-time interface satisfaction.
var _ environs.Environ = (*environ)(nil)
