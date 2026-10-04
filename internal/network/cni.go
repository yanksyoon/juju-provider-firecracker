// Package network provides the CNI boundary used by Firecracker instances.
package network

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/containernetworking/cni/libcni"
	types "github.com/containernetworking/cni/pkg/types"
	types100 "github.com/containernetworking/cni/pkg/types/100"
)

const defaultConfigPath = "/etc/cni/net.d/juju-fc.conflist"

type cniClient interface {
	AddNetworkList(context.Context, *libcni.NetworkConfigList, *libcni.RuntimeConf) (types.Result, error)
	DelNetworkList(context.Context, *libcni.NetworkConfigList, *libcni.RuntimeConf) error
}

// CNIOptions supplies injectable dependencies for tests. Production callers
// normally use NewCNIManager and the real libcni client.
type CNIOptions struct {
	CNI cniClient
}

// CNIManager owns CNI additions and deletions. The mutex deliberately covers
// the plugin call: a host-local IPAM data directory must not be changed by a
// concurrent add/delete for the same manager.
type CNIManager struct {
	mu         sync.Mutex
	configPath string
	cni        cniClient
}

// NewCNIManager creates a manager using the supplied conflist path. An empty
// path uses the system default. CNI_PATH, or the platform's normal CNI path,
// is used by libcni to locate plugins.
func NewCNIManager(configPath string) (*CNIManager, error) {
	return NewCNIManagerWithOptions(configPath, CNIOptions{})
}

// NewCNIManagerWithOptions creates a manager with an optional CNI client seam.
func NewCNIManagerWithOptions(configPath string, opts CNIOptions) (*CNIManager, error) {
	if configPath == "" {
		configPath = defaultConfigPath
	}
	if filepath.Ext(configPath) != ".conflist" {
		return nil, fmt.Errorf("CNI config must be a .conflist: %q", configPath)
	}
	if _, err := os.Stat(configPath); err != nil {
		return nil, fmt.Errorf("stat CNI config %q: %w", configPath, err)
	}
	client := opts.CNI
	if client == nil {
		paths := []string{"/opt/cni/bin", "/usr/lib/cni", "/usr/libexec/cni"}
		if configured := os.Getenv("CNI_PATH"); configured != "" {
			paths = filepath.SplitList(configured)
		}
		client = libcni.NewCNIConfig(paths, nil)
	}
	return &CNIManager{configPath: configPath, cni: client}, nil
}

// SetupNetwork adds the CNI network and returns the first allocated address
// and interface name. CNI interface names are the host-visible attachment
// names (for example, eth0 or tap0, depending on the configured plugin).
func (m *CNIManager) SetupNetwork(containerID string) (ip string, tapName string, err error) {
	if m == nil {
		return "", "", errors.New("nil CNI manager")
	}
	if strings.TrimSpace(containerID) == "" {
		return "", "", errors.New("container ID must not be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	conf, err := m.loadConfig()
	if err != nil {
		return "", "", fmt.Errorf("load CNI config: %w", err)
	}
	rt := &libcni.RuntimeConf{ContainerID: containerID, NetNS: ""}
	result, err := m.cni.AddNetworkList(context.Background(), conf, rt)
	if err != nil {
		return "", "", fmt.Errorf("add CNI network for %q: %w", containerID, err)
	}
	parsed, err := types100.NewResultFromResult(result)
	if err != nil {
		_ = m.cni.DelNetworkList(context.Background(), conf, rt)
		return "", "", fmt.Errorf("parse CNI result for %q: %w", containerID, err)
	}
	for _, addr := range parsed.IPs {
		if addr != nil && addr.Address.IP != nil {
			ip = addr.Address.IP.String()
			break
		}
	}
	for _, iface := range parsed.Interfaces {
		if iface != nil && iface.Name != "" {
			tapName = iface.Name
			break
		}
	}
	if ip == "" || tapName == "" {
		_ = m.cni.DelNetworkList(context.Background(), conf, rt)
		return "", "", fmt.Errorf("CNI result for %q has no allocated IP or interface", containerID)
	}
	return ip, tapName, nil
}

// TeardownNetwork removes a CNI network attachment. CNI DEL is idempotent for
// this boundary: missing config, namespace, interface, or IPAM state is safe
// to ignore during cleanup.
func (m *CNIManager) TeardownNetwork(containerID string) error {
	if m == nil || strings.TrimSpace(containerID) == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	conf, err := m.loadConfig()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("load CNI config for teardown: %w", err)
	}
	rt := &libcni.RuntimeConf{ContainerID: containerID, NetNS: ""}
	if err := m.cni.DelNetworkList(context.Background(), conf, rt); err != nil && !isMissingResource(err) {
		return fmt.Errorf("delete CNI network for %q: %w", containerID, err)
	}
	return nil
}

func (m *CNIManager) loadConfig() (*libcni.NetworkConfigList, error) {
	name := strings.TrimSuffix(filepath.Base(m.configPath), filepath.Ext(m.configPath))
	return libcni.LoadConfList(filepath.Dir(m.configPath), name)
}

func isMissingResource(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, phrase := range []string{"not found", "no such file", "does not exist", "already removed", "unknown container", "no network"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return errors.Is(err, os.ErrNotExist)
}
