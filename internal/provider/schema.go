package provider

import (
	"fmt"

	"github.com/juju/juju/environs/config"
	"github.com/juju/schema"
	"gopkg.in/juju/environschema.v1"
)

const providerType = "firecracker"

var configSchema = environschema.Fields{
	"firecracker-binary": {
		Description: "Path to the Firecracker binary (absolute, or bare name on PATH)",
		Type:        environschema.Tstring,
		Group:       environschema.ProviderGroup,
	},
	"kernel-image-path": {
		Description: "Path to the kernel image for Firecracker VMs",
		Type:        environschema.Tstring,
		Group:       environschema.ProviderGroup,
	},
	"rootfs-path": {
		Description: "Path to the root filesystem image for Firecracker VMs",
		Type:        environschema.Tstring,
		Group:       environschema.ProviderGroup,
	},
	"cni-config-path": {
		Description: "Path to CNI conflist for Firecracker networking",
		Type:        environschema.Tstring,
		Group:       environschema.ProviderGroup,
	},
	"cni-bin-dirs": {
		Description: "Colon-separated CNI plugin binary directories",
		Type:        environschema.Tstring,
		Group:       environschema.ProviderGroup,
	},
	"cgroup-base": {
		Description: "cgroup v2 base path for Firecracker processes",
		Type:        environschema.Tstring,
		Group:       environschema.ProviderGroup,
	},
	"config-dir": {
		Description: "Directory for per-VM Firecracker configuration files",
		Type:        environschema.Tstring,
		Group:       environschema.ProviderGroup,
	},
	"socket-dir": {
		Description: "Directory for per-VM Firecracker API sockets",
		Type:        environschema.Tstring,
		Group:       environschema.ProviderGroup,
	},
	"metadata-listen-addr": {
		Description: "Listen address for the metadata HTTP server (host:port)",
		Type:        environschema.Tstring,
		Group:       environschema.ProviderGroup,
	},
	"stop-timeout": {
		Description: "Grace period for Firecracker VM shutdown (duration string, e.g. 5s)",
		Type:        environschema.Tstring,
		Group:       environschema.ProviderGroup,
	},
	"shutdown-timeout": {
		Description: "Grace period for metadata server shutdown (duration string, e.g. 5s)",
		Type:        environschema.Tstring,
		Group:       environschema.ProviderGroup,
	},
}

var configDefaults = schema.Defaults{
	"firecracker-binary":   "firecracker",
	"cni-config-path":      "/etc/cni/net.d/juju-fc.conflist",
	"cni-bin-dirs":         "/opt/cni/bin:/usr/lib/cni:/usr/libexec/cni",
	"cgroup-base":          "/sys/fs/cgroup/juju-fc",
	"config-dir":           "/var/lib/juju-firecracker/configs",
	"socket-dir":           "/var/lib/juju-firecracker/sockets",
	"metadata-listen-addr": "127.0.0.1",
	"stop-timeout":         "5s",
	"shutdown-timeout":     "5s",
}

var configFields = func() schema.Fields {
	fs, _, err := configSchema.ValidationSchema()
	if err != nil {
		panic(fmt.Sprintf("invalid config schema: %v", err))
	}
	return fs
}()

var configChecker = schema.FieldMap(configFields, configDefaults)

// Validate returns a validated config or an error.
func (ep *environProvider) Validate(cfg, old *config.Config) (*config.Config, error) {
	_ = old // Firecracker provider does not constrain transitions yet

	attrs := cfg.AllAttrs()
	validated, err := configChecker.Coerce(attrs, nil)
	if err != nil {
		return nil, fmt.Errorf("validate firecracker config: %w", err)
	}
	coerced := validated.(map[string]interface{})

	// Use the typed Config to run full validation.
	fcCfg, err := NewConfig(coerced)
	if err != nil {
		return nil, err
	}
	if err := fcCfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate firecracker config: %w", err)
	}

	return cfg.Apply(coerced)
}
