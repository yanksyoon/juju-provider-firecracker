package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds every operator-configurable value for the Firecracker provider
// with explicit defaults. Zero values after applying defaults indicate a
// required field was not supplied, which Validate rejects.
type Config struct {
	// Paths
	FirecrackerBinary string
	KernelImagePath   string
	RootFSPath        string
	CNIConfigPath     string
	CNIBinDirs        []string
	CgroupBase        string
	ConfigDir         string
	SocketDir         string

	// Network
	MetadataListenAddr string

	// CrossControllerSettings is an optional secret-free JSON contract.
	CrossControllerSettings string

	// Timeouts
	StopTimeout     time.Duration
	ShutdownTimeout time.Duration
}

// DefaultConfig returns a Config with backwards-compatible defaults.
func DefaultConfig() Config {
	return Config{
		FirecrackerBinary:  "firecracker",
		CNIConfigPath:      "/etc/cni/net.d/juju-fc.conflist",
		CNIBinDirs:         []string{"/opt/cni/bin", "/usr/lib/cni", "/usr/libexec/cni"},
		CgroupBase:         "/sys/fs/cgroup/juju-fc",
		ConfigDir:          "/var/lib/juju-firecracker/configs",
		SocketDir:          "/var/lib/juju-firecracker/sockets",
		MetadataListenAddr: "127.0.0.1:8080",
		StopTimeout:        5 * time.Second,
		ShutdownTimeout:    5 * time.Second,
	}
}

// envOverrides maps environment variable names to setter functions. Only
// non-empty env vars override the config; empty vars are ignored.
var envOverrides = []struct {
	key string
	set func(*Config, string) error
}{
	{"JUJU_FC_FIRECRACKER_BINARY", func(c *Config, v string) error { c.FirecrackerBinary = v; return nil }},
	{"JUJU_FC_KERNEL_IMAGE_PATH", func(c *Config, v string) error { c.KernelImagePath = v; return nil }},
	{"JUJU_FC_ROOTFS_PATH", func(c *Config, v string) error { c.RootFSPath = v; return nil }},
	{"JUJU_FC_CNI_CONFIG_PATH", func(c *Config, v string) error { c.CNIConfigPath = v; return nil }},
	{"CNI_PATH", func(c *Config, v string) error { c.CNIBinDirs = filepath.SplitList(v); return nil }},
	{"JUJU_FC_CGROUP_BASE", func(c *Config, v string) error { c.CgroupBase = v; return nil }},
	{"JUJU_FC_CONFIG_DIR", func(c *Config, v string) error { c.ConfigDir = v; return nil }},
	{"JUJU_FC_SOCKET_DIR", func(c *Config, v string) error { c.SocketDir = v; return nil }},
	{"JUJU_FC_METADATA_LISTEN", func(c *Config, v string) error { c.MetadataListenAddr = v; return nil }},
	{"JUJU_FC_STOP_TIMEOUT", func(c *Config, v string) error {
		d, err := parseDuration(v)
		if err != nil {
			return fmt.Errorf("JUJU_FC_STOP_TIMEOUT: %w", err)
		}
		c.StopTimeout = d
		return nil
	}},
	{"JUJU_FC_SHUTDOWN_TIMEOUT", func(c *Config, v string) error {
		d, err := parseDuration(v)
		if err != nil {
			return fmt.Errorf("JUJU_FC_SHUTDOWN_TIMEOUT: %w", err)
		}
		c.ShutdownTimeout = d
		return nil
	}},
}

// NewConfig builds a Config from model config attributes, applying env var
// overrides and then defaults. Precedence: model config > env vars > defaults.
func NewConfig(attrs map[string]interface{}) (Config, error) {
	cfg := DefaultConfig()

	// Apply environment variable overrides first (lowest priority).
	for _, e := range envOverrides {
		if v := os.Getenv(e.key); v != "" {
			if err := e.set(&cfg, v); err != nil {
				return Config{}, err
			}
		}
	}

	// Apply model config overrides on top (highest priority).
	if v, ok := attrs["firecracker-binary"].(string); ok && v != "" {
		cfg.FirecrackerBinary = v
	}
	if v, ok := attrs["kernel-image-path"].(string); ok && v != "" {
		cfg.KernelImagePath = v
	}
	if v, ok := attrs["rootfs-path"].(string); ok && v != "" {
		cfg.RootFSPath = v
	}
	if v, ok := attrs["cni-config-path"].(string); ok && v != "" {
		cfg.CNIConfigPath = v
	}
	if v, ok := attrs["cni-bin-dirs"].(string); ok && v != "" {
		cfg.CNIBinDirs = filepath.SplitList(v)
	}
	if v, ok := attrs["cgroup-base"].(string); ok && v != "" {
		cfg.CgroupBase = v
	}
	if v, ok := attrs["config-dir"].(string); ok && v != "" {
		cfg.ConfigDir = v
	}
	if v, ok := attrs["socket-dir"].(string); ok && v != "" {
		cfg.SocketDir = v
	}
	if v, ok := attrs["cross-controller-settings"].(string); ok && v != "" {
		cfg.CrossControllerSettings = v
	}
	if v, ok := attrs["metadata-listen-addr"].(string); ok && v != "" {
		cfg.MetadataListenAddr = v
	}
	if v, ok := attrs["stop-timeout"].(string); ok && v != "" {
		d, err := parseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("stop-timeout: %w", err)
		}
		cfg.StopTimeout = d
	}
	if v, ok := attrs["shutdown-timeout"].(string); ok && v != "" {
		d, err := parseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("shutdown-timeout: %w", err)
		}
		cfg.ShutdownTimeout = d
	}

	return cfg, nil
}

// Validate checks that the config is safe and complete. It rejects missing
// required fields, unsafe paths, and invalid network addresses.
func (c Config) Validate() error {
	// Required fields — collect all missing before returning.
	var missing []string
	if c.KernelImagePath == "" {
		missing = append(missing, "kernel-image-path")
	}
	if c.RootFSPath == "" {
		missing = append(missing, "rootfs-path")
	}
	if len(missing) > 0 {
		return fmt.Errorf("required field(s) missing: %s", strings.Join(missing, ", "))
	}

	// Unsafe paths. Fail closed on relative, empty, or non-canonical paths
	// that could resolve unpredictably at runtime.
	for _, f := range []struct {
		name  string
		value string
	}{
		{"config-dir", c.ConfigDir},
		{"socket-dir", c.SocketDir},
		{"cgroup-base", c.CgroupBase},
		{"cni-config-path", c.CNIConfigPath},
	} {
		if err := validateSafePath(f.name, f.value, true); err != nil {
			return err
		}
	}

	// Firecracker binary can be a bare name on PATH; only validate absolute.
	if filepath.IsAbs(c.FirecrackerBinary) {
		if err := validateSafePath("firecracker-binary", c.FirecrackerBinary, false); err != nil {
			return err
		}
	}

	// Kernel and rootfs must be absolute and safe.
	for _, f := range []struct {
		name  string
		value string
	}{
		{"kernel-image-path", c.KernelImagePath},
		{"rootfs-path", c.RootFSPath},
	} {
		if err := validateSafePath(f.name, f.value, true); err != nil {
			return err
		}
	}

	// Metadata listen address must be a valid host:port.
	if c.MetadataListenAddr != "" {
		if _, _, err := parseHostPort(c.MetadataListenAddr); err != nil {
			return fmt.Errorf("metadata-listen-addr: %w", err)
		}
	}

	// Timeouts must be > 0.
	if c.StopTimeout <= 0 {
		return fmt.Errorf("stop-timeout must be positive, got %s", c.StopTimeout)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown-timeout must be positive, got %s", c.ShutdownTimeout)
	}
	if c.CrossControllerSettings != "" {
		if _, err := ParseCrossControllerConfig([]byte(c.CrossControllerSettings)); err != nil {
			return fmt.Errorf("cross-controller-settings: %w", err)
		}
	}

	// CNI config must have .conflist extension.
	if filepath.Ext(c.CNIConfigPath) != ".conflist" {
		return fmt.Errorf("cni-config-path must be a .conflist: %q", c.CNIConfigPath)
	}

	return nil
}

func validateSafePath(name, path string, requireAbs bool) error {
	if path == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if requireAbs && !filepath.IsAbs(path) {
		return fmt.Errorf("%s must be an absolute path: %q", name, path)
	}
	clean := filepath.Clean(path)
	if clean != path {
		return fmt.Errorf("%s is not canonical: %q (canonical: %q)", name, path, clean)
	}
	// Reject traversal components even after Clean, as a defense-in-depth
	// measure against crafted inputs.
	if strings.Contains(clean, "..") {
		return fmt.Errorf("%s contains traversal: %q", name, path)
	}
	return nil
}

func parseHostPort(addr string) (host string, port int, err error) {
	// net.JoinHostPort handles IPv6 bracketing; parse it back.
	hostStr, portStr, splitErr := splitHostPort(addr)
	if splitErr != nil {
		return "", 0, splitErr
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p < 0 || p > 65535 {
		return "", 0, fmt.Errorf("invalid port %q", portStr)
	}
	return hostStr, p, nil
}

func splitHostPort(addr string) (string, string, error) {
	// Handle IPv6: [::1]:8080
	if strings.HasPrefix(addr, "[") {
		end := strings.Index(addr, "]")
		if end < 0 {
			return "", "", fmt.Errorf("malformed IPv6 address %q", addr)
		}
		host := addr[1:end]
		rest := addr[end+1:]
		if !strings.HasPrefix(rest, ":") {
			return "", "", fmt.Errorf("missing port in %q", addr)
		}
		return host, rest[1:], nil
	}
	// IPv4 or hostname: host:port
	lastColon := strings.LastIndex(addr, ":")
	if lastColon < 0 {
		return "", "", fmt.Errorf("missing port in %q", addr)
	}
	return addr[:lastColon], addr[lastColon+1:], nil
}

func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		// Accept integer seconds as a convenience.
		if secs, err2 := strconv.Atoi(s); err2 == nil {
			return time.Duration(secs) * time.Second, nil
		}
	}
	return d, err
}
