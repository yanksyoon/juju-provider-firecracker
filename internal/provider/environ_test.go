package provider

import (
	"errors"
	"testing"

	"github.com/juju/juju/cloud"
	"github.com/juju/juju/core/constraints"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/version/v2"
)

func TestEnvironNewAndConfig(t *testing.T) {
	net := &fakeNetwork{}
	vm := &fakeVM{}
	cfg := testConfig()
	cfg.ConfigDir = t.TempDir()
	cfg.SocketDir = t.TempDir()
	p, err := New(Options{
		Config:   cfg,
		Network:  net,
		Metadata: &fakeMetadata{},
		VM:       vm,
	})
	if err != nil {
		t.Fatal(err)
	}
	jujuCfg, err := config.New(config.NoDefaults, map[string]interface{}{
		"name":              "test",
		"type":              "firecracker",
		"uuid":              "00000000-0000-0000-0000-000000000000",
		"kernel-image-path": "/tmp/kernel",
		"rootfs-path":       "/tmp/rootfs",
	})
	if err != nil {
		t.Fatal(err)
	}
	env := &environ{
		FirecrackerProvider: p,
		cfg:                 jujuCfg,
		provider:            &environProvider{},
	}
	if got := env.Config(); got == nil {
		t.Fatal("Config() returned nil")
	}

	newCfg, err := config.New(config.NoDefaults, map[string]interface{}{
		"name":              "test2",
		"type":              "firecracker",
		"uuid":              "11111111-1111-1111-1111-111111111111",
		"kernel-image-path": "/tmp/kernel",
		"rootfs-path":       "/tmp/rootfs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.SetConfig(newCfg); err != nil {
		t.Fatal(err)
	}
}

func TestEnvironProviderInterface(t *testing.T) {
	net := &fakeNetwork{ip: "10.0.0.1", tap: "tap0"}
	vm := &fakeVM{}
	cfg := testConfig()
	cfg.ConfigDir = t.TempDir()
	cfg.SocketDir = t.TempDir()
	p, err := New(Options{
		Config:   cfg,
		Network:  net,
		Metadata: &fakeMetadata{},
		VM:       vm,
	})
	if err != nil {
		t.Fatal(err)
	}
	jujuCfg, err := config.New(config.NoDefaults, map[string]interface{}{
		"name":              "test",
		"type":              "firecracker",
		"uuid":              "00000000-0000-0000-0000-000000000000",
		"kernel-image-path": "/tmp/kernel",
		"rootfs-path":       "/tmp/rootfs",
	})
	if err != nil {
		t.Fatal(err)
	}
	env := &environ{
		FirecrackerProvider: p,
		cfg:                 jujuCfg,
		provider:            &environProvider{},
	}

	if env.PrepareForBootstrap(nil, "test-controller") != nil {
		t.Fatal("PrepareForBootstrap should succeed")
	}

	instances, err := env.ControllerInstances(nil, "uuid")
	if err == nil {
		t.Fatal("expected ErrNoInstances when no instances exist")
	}
	if !errors.Is(err, environs.ErrNoInstances) {
		t.Fatalf("expected ErrNoInstances, got %v", err)
	}
	_ = instances

	if err := env.Create(nil, environs.CreateParams{}); err != nil {
		t.Fatal(err)
	}

	validator, err := env.ConstraintsValidator(nil)
	if err != nil || validator == nil {
		t.Fatalf("ConstraintsValidator: err=%v validator=%v", err, validator)
	}

	if err := env.PrecheckInstance(nil, environs.PrecheckInstanceParams{}); err != nil {
		t.Fatal(err)
	}

	types, err := env.InstanceTypes(nil, constraints.Value{})
	if err != nil || len(types.InstanceTypes) != 0 {
		t.Fatalf("InstanceTypes: err=%v types=%v", err, types)
	}

	if err := env.AdoptResources(nil, "uuid", version.Number{}); err != nil {
		t.Fatal(err)
	}

	storageTypes, err := env.StorageProviderTypes()
	if err != nil || len(storageTypes) != 0 {
		t.Fatalf("StorageProviderTypes: err=%v types=%v", err, storageTypes)
	}
}

func TestEnvironProviderOpen(t *testing.T) {
	ep := &environProvider{}

	if ep.Version() != 0 {
		t.Fatal("version should be 0")
	}
	if ep.CloudSchema() != nil {
		t.Fatal("cloud schema should be nil")
	}
	if err := ep.Ping(nil, ""); err != nil {
		t.Fatal(err)
	}

	credSchemas := ep.CredentialSchemas()
	if len(credSchemas) != 1 {
		t.Fatalf("expected only the empty credential schema, got %#v", credSchemas)
	}

	cred, err := ep.DetectCredentials("")
	if err != nil {
		t.Fatal(err)
	}
	if cred == nil || cred.AuthCredentials["default"].AuthType() != cloud.EmptyAuthType {
		t.Fatalf("expected default empty credential, got %#v", cred)
	}

	finalized, err := ep.FinalizeCredential(nil, environs.FinalizeCredentialParams{})
	if err != nil {
		t.Fatal(err)
	}
	if finalized == nil {
		t.Fatal("expected a credential")
	}
}

func TestProviderRegistrationAndLocalRegion(t *testing.T) {
	ep, err := environs.Provider("firecracker")
	if err != nil {
		t.Fatalf("provider registration: %v", err)
	}

	detector, ok := ep.(environs.CloudRegionDetector)
	if !ok {
		t.Fatal("registered provider does not detect cloud regions")
	}
	regions, err := detector.DetectRegions()
	if err != nil {
		t.Fatalf("detect regions: %v", err)
	}
	if len(regions) != 1 || regions[0].Name != "local" {
		t.Fatalf("unexpected regions: %#v", regions)
	}
	if regions[0].Endpoint != "local" || regions[0].IdentityEndpoint != "" || regions[0].StorageEndpoint != "" {
		t.Fatalf("unexpected local region endpoints: %#v", regions[0])
	}
	if got := ep.CredentialSchemas(); len(got) != 1 {
		t.Fatalf("Firecracker should expose only the empty credential schema: %#v", got)
	}
}
