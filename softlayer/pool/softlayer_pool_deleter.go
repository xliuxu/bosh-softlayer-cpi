package pool

import (
	"fmt"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	strfmt "github.com/go-openapi/strfmt"

	slhelper "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common/helper"

	operations "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/pool/client/vm"
	slc "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"

	"github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/pool/models"
)

type softLayerPoolDeleter struct {
	client slc.Client
	logger boshlog.Logger
}

func NewSoftLayerPoolDeleter(client slc.Client, logger boshlog.Logger) VMDeleter {
	return &softLayerPoolDeleter{
		client:       client,
		logger:                logger,
	}
}

func (c *softLayerPoolDeleter) Delete(cid int) error {
	_, err := c.client.GetVMByCid(operations.NewGetVMByCidParams().WithCid(int32(cid)))
	if err != nil {
		_, ok := err.(*operations.GetVMByCidNotFound)
		if ok {
			virtualGuest, err := slhelper.GetObjectDetailsOnVirtualGuest(c.client, cid)
			if err != nil {
				return bosherr.WrapError(err, fmt.Sprintf("Getting virtual guest %d details from SoftLayer", cid))
			}

			slPoolVm := &models.VM{
				Cid:         int32(cid),
				CPU:         int32(virtualGuest.StartCpus),
				MemoryMb:    int32(virtualGuest.MaxMemory),
				IP:          strfmt.IPv4(virtualGuest.PrimaryBackendIpAddress),
				Hostname:    virtualGuest.FullyQualifiedDomainName,
				PrivateVlan: int32(virtualGuest.PrimaryBackendNetworkComponent.NetworkVlan.Id),
				PublicVlan:  int32(virtualGuest.PrimaryNetworkComponent.NetworkVlan.Id),
				State:       models.StateFree,
			}
			_, err = c.client.AddVM(operations.NewAddVMParams().WithBody(slPoolVm))
			if err != nil {
				return bosherr.WrapError(err, fmt.Sprintf("Adding vm %d to pool", cid))
			}
			return nil
		}
		return bosherr.WrapError(err, "Removing vm from pool")
	}

	free := models.VMState{
		State: models.StateFree,
	}
	_, err = c.client.UpdateVMWithState(operations.NewUpdateVMWithStateParams().WithBody(&free).WithCid(int32(cid)))
	if err != nil {
		return bosherr.WrapErrorf(err, "Updating state of vm %d in pool to free", cid)
	}

	return nil
}