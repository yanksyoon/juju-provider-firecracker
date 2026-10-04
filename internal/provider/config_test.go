package provider

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.FirecrackerBinary != "firecracker" {
		t.Fatalf("FirecrackerBinary = %q", cfg.FirecrackerBinary)
	}
	if cfg.CNIConfigPath != "/etc/cni/net.d/juju-fc.conflist" {
		t.Fatalf("CNIConfigPath = %q", cfg.CNIConfigPath)
	}
	if cfg.CgroupBase != "/sys/fs/cgroup/juju-fc" {
		t.Fatalf("CgroupBase = %q", cfg.CgroupBase)
	}
	if cfg.ConfigDir != "/var/lib/juju-firecracker/configs" {
		t.Fatalf("ConfigDir = %q", cfg.ConfigDir)
	}
	if cfg.SocketDir != "/var/lib/juju-firecracker/sockets" {
		t.Fatalf("SocketDir = %q", cfg.SocketDir)
	}
	if cfg.MetadataListenAddr != "127.0.0.1:8080" {
		t.Fatalf("MetadataListenAddr = %q", cfg.MetadataListenAddr)
	}
	if cfg.StopTimeout != 5*time.Second {
		t.Fatalf("StopTimeout = %s", cfg.StopTimeout)
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Fatalf("ShutdownTimeout = %s", cfg.ShutdownTimeout)
	}
	if len(cfg.CNIBinDirs) != 3 {
		t.Fatalf("CNIBinDirs = %v", cfg.CNIBinDirs)
	}
}

func TestNewConfigModelAttrsOverride(t *testing.T) {
	cfg, err := NewConfig(map[string]interface{}{
		"kernel-image-path":    "/opt/kernel/vmlinux",
		"rootfs-path":          "/opt/rootfs/ubuntu.ext4",
		"cni-config-path":      "/tmp/custom.conflist",
		"cgroup-base":          "/sys/fs/cgroup/fc-test",
		"config-dir":           "/var/lib/fc-custom/configs",
		"socket-dir":           "/var/lib/fc-custom/sockets",
		"metadata-listen-addr": "127.0.0.1:8080",
		"stop-timeout":         "10s",
		"shutdown-timeout":     "3s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KernelImagePath != "/opt/kernel/vmlinux" {
		t.Fatalf("kernel = %q", cfg.KernelImagePath)
	}
	if cfg.CNIConfigPath != "/tmp/custom.conflist" {
		t.Fatalf("cni = %q", cfg.CNIConfigPath)
	}
	if cfg.ConfigDir != "/var/lib/fc-custom/configs" {
		t.Fatalf("config-dir = %q", cfg.ConfigDir)
	}
	if cfg.MetadataListenAddr != "127.0.0.1:8080" {
		t.Fatalf("listen = %q", cfg.MetadataListenAddr)
	}
	if cfg.StopTimeout != 10*time.Second {
		t.Fatalf("stop = %s", cfg.StopTimeout)
	}
	if cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("shutdown = %s", cfg.ShutdownTimeout)
	}
}

func TestNewConfigEnvOverride(t *testing.T) {
	os.Setenv("JUJU_FC_CONFIG_DIR", "/env/configs")
	os.Setenv("JUJU_FC_STOP_TIMEOUT", "30")
	defer func() {
		os.Unsetenv("JUJU_FC_CONFIG_DIR")
		os.Unsetenv("JUJU_FC_STOP_TIMEOUT")
	}()

	cfg, err := NewConfig(map[string]interface{}{
		"kernel-image-path": "/kernel",
		"rootfs-path":       "/rootfs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigDir != "/env/configs" {
		t.Fatalf("env config-dir = %q", cfg.ConfigDir)
	}
	if cfg.StopTimeout != 30*time.Second {
		t.Fatalf("env stop-timeout = %s", cfg.StopTimeout)
	}
}

func TestNewConfigPrecedence(t *testing.T) {
	// Model config should beat env var.
	os.Setenv("JUJU_FC_CGROUP_BASE", "/from-env")
	defer os.Unsetenv("JUJU_FC_CGROUP_BASE")

	cfg, err := NewConfig(map[string]interface{}{
		"kernel-image-path": "/kernel",
		"rootfs-path":       "/rootfs",
		"cgroup-base":       "/from-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CgroupBase != "/from-model" {
		t.Fatalf("cgroup-base = %q (model should beat env)", cfg.CgroupBase)
	}
}

func TestMetadataListenPrecedence(t *testing.T) {
	os.Setenv("JUJU_FC_METADATA_LISTEN", "127.0.0.1:9090")
	defer os.Unsetenv("JUJU_FC_METADATA_LISTEN")

	cfg, err := NewConfig(map[string]interface{}{
		"kernel-image-path":    "/kernel",
		"rootfs-path":          "/rootfs",
		"metadata-listen-addr": "127.0.0.1:9191",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetadataListenAddr != "127.0.0.1:9191" {
		t.Fatalf("metadata listen address = %q (model should beat env)", cfg.MetadataListenAddr)
	}

	cfg, err = NewConfig(map[string]interface{}{
		"kernel-image-path": "/kernel",
		"rootfs-path":       "/rootfs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetadataListenAddr != "127.0.0.1:9090" {
		t.Fatalf("metadata listen address = %q (env should beat default)", cfg.MetadataListenAddr)
	}
}

func TestValidateMissingRequired(t *testing.T) {
	cfg := DefaultConfig()
	cfg.KernelImagePath = ""
	cfg.RootFSPath = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing kernel/rootfs")
	}
	if !strings.Contains(err.Error(), "kernel-image-path") || !strings.Contains(err.Error(), "rootfs-path") {
		t.Fatalf("error should mention both missing fields: %v", err)
	}
}

func TestValidateUnsafePath(t *testing.T) {
	tests := []struct {
		name  string
		field func(*Config)
	}{
		{"config-dir", func(c *Config) { c.ConfigDir = "../escape" }},
		{"socket-dir", func(c *Config) { c.SocketDir = "/etc//passwd" }},
		{"cgroup-base", func(c *Config) { c.CgroupBase = "/sys/../root" }},
		{"cni-config-path", func(c *Config) { c.CNIConfigPath = "/tmp/../../etc/shadow.conflist" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.KernelImagePath = "/kernel"
			cfg.RootFSPath = "/rootfs"
			tt.field(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateMetadataAddr(t *testing.T) {
	tests := []struct {
		name  string
		addr  string
		valid bool
	}{
		{"ok host:port", "127.0.0.1:8080", true},
		{"ok bare host", "127.0.0.1", false}, // missing port
		{"bad port", "127.0.0.1:99999", false},
		{"invalid", "not-an-address", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.KernelImagePath = "/kernel"
			cfg.RootFSPath = "/rootfs"
			cfg.MetadataListenAddr = tt.addr
			err := cfg.Validate()
			if tt.valid && err != nil {
				t.Fatalf("expected valid, got: %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestValidateCNIConfigExtension(t *testing.T) {
	cfg := DefaultConfig()
	cfg.KernelImagePath = "/kernel"
	cfg.RootFSPath = "/rootfs"
	cfg.CNIConfigPath = "/tmp/not-a-conflist.json"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for non-conflist CNI config")
	}
}

func TestValidateNegativeTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.KernelImagePath = "/kernel"
	cfg.RootFSPath = "/rootfs"
	cfg.StopTimeout = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for negative timeout")
	}
}

func TestParseDurationSeconds(t *testing.T) {
	d, err := parseDuration("15")
	if err != nil {
		t.Fatal(err)
	}
	if d != 15*time.Second {
		t.Fatalf("got %s", d)
	}
}

func TestParseDurationStd(t *testing.T) {
	d, err := parseDuration("1m30s")
	if err != nil {
		t.Fatal(err)
	}
	if d != 90*time.Second {
		t.Fatalf("got %s", d)
	}
}

func TestCNIBinDirsFromModelConfig(t *testing.T) {
	cfg, err := NewConfig(map[string]interface{}{
		"kernel-image-path": "/kernel",
		"rootfs-path":       "/rootfs",
		"cni-bin-dirs":      "/custom/bin:/opt/cni:/usr/lib/cni",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CNIBinDirs) != 3 || cfg.CNIBinDirs[0] != "/custom/bin" {
		t.Fatalf("CNIBinDirs = %v", cfg.CNIBinDirs)
	}
}

func TestCNIBinDirsFromEnv(t *testing.T) {
	os.Setenv("CNI_PATH", "/env/cni:/env/alt")
	defer os.Unsetenv("CNI_PATH")

	cfg, err := NewConfig(map[string]interface{}{
		"kernel-image-path": "/kernel",
		"rootfs-path":       "/rootfs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CNIBinDirs) != 2 {
		t.Fatalf("CNIBinDirs = %v", cfg.CNIBinDirs)
	}
}

func TestCrossControllerSettingsRoundTripAndValidation(t *testing.T) {
	settings := `{"enabled":true,"controller_name":"disposable-k8s","api_addresses":["127.0.0.1:17070"],"model_name":"fc-compat","offer_name":"fc-offer","cleanup":true}`
	cfg, err := NewConfig(map[string]interface{}{"kernel-image-path": "/kernel", "rootfs-path": "/rootfs", "metadata-listen-addr": "127.0.0.1:8080", "cross-controller-settings": settings})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CrossControllerSettings != settings {
		t.Fatalf("settings were not preserved: %q", cfg.CrossControllerSettings)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCrossControllerSettingsMalformedFailsClosed(t *testing.T) {
	cfg, err := NewConfig(map[string]interface{}{"kernel-image-path": "/kernel", "rootfs-path": "/rootfs", "metadata-listen-addr": "127.0.0.1:8080", "cross-controller-settings": `{"enabled":true}`})
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected malformed cross-controller settings to fail validation")
	}
}
