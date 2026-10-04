package provider_test

import (
	"testing"

	_ "github.com/canonical/juju-provider-firecracker/provider"
	"github.com/juju/juju/environs"
)

func TestPublicImportRegistersFirecracker(t *testing.T) {
	registered, err := environs.Provider("firecracker")
	if err != nil {
		t.Fatalf("provider registration: %v", err)
	}
	regions, ok := registered.(environs.CloudRegionDetector)
	if !ok {
		t.Fatal("registered provider does not implement CloudRegionDetector")
	}
	got, err := regions.DetectRegions()
	if err != nil {
		t.Fatalf("detect regions: %v", err)
	}
	if len(got) != 1 || got[0].Name != "local" {
		t.Fatalf("unexpected regions: %#v", got)
	}
}
