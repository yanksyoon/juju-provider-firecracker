// Package provider is the public Juju integration boundary for Firecracker.
//
// A Juju fork must blank-import this package from its in-process provider
// registry (internal/provider/all). The standalone juju-firecracker command
// is not a plugin and cannot register a provider in another process.
package provider

import _ "github.com/canonical/juju-provider-firecracker/internal/provider"
