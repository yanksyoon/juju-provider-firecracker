package provider

import (
	"github.com/juju/juju/core/instance"
	corenetwork "github.com/juju/juju/core/network"
	"github.com/juju/juju/core/status"
	jujucontext "github.com/juju/juju/environs/context"
)

// FirecrackerInstance is the provider-facing view of a running microVM.
type FirecrackerInstance struct {
	id     instance.Id
	ip     string
	status status.Status
}

func (i *FirecrackerInstance) Id() instance.Id { return i.id }
func (i *FirecrackerInstance) Status(_ jujucontext.ProviderCallContext) instance.Status {
	return instance.Status{Status: i.status}
}
func (i *FirecrackerInstance) Addresses(_ jujucontext.ProviderCallContext) (corenetwork.ProviderAddresses, error) {
	if i.ip == "" {
		return nil, nil
	}
	return corenetwork.ProviderAddresses{{MachineAddress: corenetwork.MachineAddress{Value: i.ip}}}, nil
}
