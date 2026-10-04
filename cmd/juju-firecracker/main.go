// Command juju-firecracker registers the Firecracker provider with Juju
// and then blocks forever. Juju discovers providers by invoking a
// provider-registration binary that must stay running.
//
// Build: go build -o /usr/local/bin/juju-firecracker ./cmd/juju-firecracker
// Usage: juju-firecracker
package main

import (
	"os"
	"os/signal"
	"syscall"

	_ "github.com/canonical/juju-provider-firecracker/internal/provider"
)

func main() {
	// Registration happens via provider.init(). This process must stay
	// alive so Juju can reach it.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}
