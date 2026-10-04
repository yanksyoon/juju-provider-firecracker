package provider

import (
	"context"
	"fmt"

	"github.com/juju/jsonschema"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"

	jujucontext "github.com/juju/juju/environs/context"
)

// environProvider implements environs.CloudEnvironProvider.
type environProvider struct{}

func init() {
	environs.RegisterProvider(providerType, &environProvider{})
}

// Version is always 0 for this provider.
func (ep *environProvider) Version() int { return 0 }

// CloudSchema returns nil — Firecracker does not support custom clouds.
func (ep *environProvider) CloudSchema() *jsonschema.Schema { return nil }

// Ping verifies the endpoint is valid. Firecracker runs locally, so the
// endpoint is unused and Ping returns nil.
func (ep *environProvider) Ping(_ jujucontext.ProviderCallContext, _ string) error { return nil }

// PrepareConfig prepares the configuration for a new model.
func (ep *environProvider) PrepareConfig(args environs.PrepareConfigParams) (*config.Config, error) {
	return args.Config, nil
}

// CredentialSchemas returns the supported credential schemas. Firecracker
// runs locally and has no cloud credentials.
func (ep *environProvider) CredentialSchemas() map[cloud.AuthType]cloud.CredentialSchema {
	return map[cloud.AuthType]cloud.CredentialSchema{}
}

// DetectRegions reports the local region used by the Firecracker provider.
// Firecracker has no remote cloud endpoint, so the region deliberately has no
// endpoint fields. The first region is the default selected by Juju bootstrap.
func (ep *environProvider) DetectRegions() ([]cloud.Region, error) {
	return []cloud.Region{{Name: "local", Endpoint: "local"}}, nil
}

// DetectCredentials returns an empty credential — Firecracker has no remote auth.
// Returning the credential is important because Juju still asks for a
// credential during bootstrap even when the provider declares no schemas.
func (ep *environProvider) DetectCredentials(_ string) (*cloud.CloudCredential, error) {
	return cloud.NewEmptyCloudCredential(), nil
}

// FinalizeCredential is a no-op for Firecracker.
func (ep *environProvider) FinalizeCredential(
	_ environs.FinalizeCredentialContext,
	_ environs.FinalizeCredentialParams,
) (*cloud.Credential, error) {
	cred := cloud.NewCredential(cloud.EmptyAuthType, nil)
	return &cred, nil
}

// Open returns a new Firecracker environ from the configuration.
func (ep *environProvider) Open(ctx context.Context, args environs.OpenParams) (environs.Environ, error) {
	cfg := args.Config
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}

	fcCfg, err := NewConfig(cfg.AllAttrs())
	if err != nil {
		return nil, fmt.Errorf("build firecracker config: %w", err)
	}
	if err := fcCfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate firecracker config: %w", err)
	}

	return newEnviron(cfg, fcCfg)
}
