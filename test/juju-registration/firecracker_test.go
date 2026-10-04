//go:build juju_firecracker_bundle

package all

import (
	"testing"

	"github.com/juju/juju/environs"
)

func TestFirecrackerProviderRegistrationAndBootstrapResolution(t *testing.T) {
	provider, err := environs.Provider("firecracker")
	if err != nil {
		t.Fatalf("firecracker provider is not registered: %v", err)
	}

	detector, ok := provider.(environs.CloudRegionDetector)
	if !ok {
		t.Fatal("firecracker provider does not implement CloudRegionDetector")
	}
	regions, err := detector.DetectRegions()
	if err != nil {
		t.Fatalf("detect regions: %v", err)
	}
	if len(regions) != 1 || regions[0].Name != "local" || regions[0].Endpoint != "local" {
		t.Fatalf("unexpected regions: %#v", regions)
	}
	if got := provider.CredentialSchemas(); len(got) != 0 {
		t.Fatalf("firecracker unexpectedly requires credentials: %#v", got)
	}
}
