package vm

import (
	"net"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"
	bslcstem "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/stemcell"
	sl "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"

	"github.com/softlayer/softlayer-go/datatypes"
	"github.com/cloudfoundry/bosh-softlayer-cpi/util"
)

type SoftLayerVirtualGuestCreator struct {
	client                 sl.Client
	agentOptions           AgentOptions
	logger                 boshlog.Logger
	vmFinder               VMFinder
	featureOptions         FeatureOptions
}

func NewSoftLayerCreator(client sl.Client, vmFinder VMFinder, agentOptions AgentOptions, featureOptions FeatureOptions, logger boshlog.Logger) VMCreator {
	return &SoftLayerVirtualGuestCreator{
		client: 	 client,
		agentOptions:    agentOptions,
		logger:          logger,
		vmFinder:        vmFinder,
		featureOptions:  featureOptions,
	}
}

func (c *SoftLayerVirtualGuestCreator) Create(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	for _, network := range networks {
		switch network.Type {
		case "dynamic":
			if cloudProps.DisableOsReload || c.featureOptions.DisableOsReload {
				return c.createBySoftlayer(agentID, stemcell, cloudProps, networks, env)
			}
			if len(network.IP) == 0 {
				return c.createBySoftlayer(agentID, stemcell, cloudProps, networks, env)
			}
			return c.createByOSReload(agentID, stemcell, cloudProps, networks, env)
		case "vip":
			return nil, bosherr.Error("SoftLayer Not Support VIP netowrk !!!")
		default:
			continue
		}
	}

	return nil, bosherr.Error("Virtual guests must have exactly one dynamic network !")
}

func (c *SoftLayerVirtualGuestCreator) createBySoftlayer(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	var virtualGuest datatypes.Virtual_Guest
	var err error

	virtualGuestTemplate := GenerateInstanceCreationTemplate(&datatypes.Virtual_Guest{}, stemcell.Uuid(), cloudProps, networks)
	virtualGuest, err = c.client.CreateInstance(virtualGuestTemplate)
	if err != nil {
		return nil, bosherr.WrapError(err, "Creating virtual guest from SoftLayer")
	}

	if virtualGuest.Id == nil {
		return nil, bosherr.Error("Creating virtual guest from SoftLayer Failed!!!")
	}

	if cloudProps.EphemeralDiskSize > 0 {
		err = c.client.AttachSecondDiskToInstance(*virtualGuest.Id, cloudProps.EphemeralDiskSize)
		if err != nil {
			return nil, bosherr.WrapErrorf(err, "Attaching second disk to virtual guest with id of `%d`", *virtualGuest.Id)
		}
	}

	vm, found := c.vmFinder.Find(*virtualGuest.Id)
	if !found {
		return nil, bosherr.Errorf("Creating VM with id of `%d`", *virtualGuest.Id)
	}

	return PostConfigVM(vm, agentID, cloudProps, networks, c.agentOptions, env)
}

func (c *SoftLayerVirtualGuestCreator) createByOSReload(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	var virtualGuest datatypes.Virtual_Guest
	var err error

	for _, network := range networks {
		switch network.Type {
		case "dynamic":
			if util.IsPrivateSubnet(net.ParseIP(network.IP)) {
				virtualGuest, err = c.client.GetInstanceByPrimaryBackendIpAddress(network.IP)
			} else {
				virtualGuest, err = c.client.GetInstanceByPrimaryIpAddress(network.IP)
			}
			if err != nil {
				return nil, bosherr.WrapErrorf(err, "Finding virtual guest by ip address: %s", network.IP)
			}
		case "manual":
			continue
		default:
			return nil, bosherr.Errorf("Unexpected network type: %s", network.Type)
		}
	}

	err = c.client.ReloadInstance(*virtualGuest.Id, stemcell.ID())
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Reloading virutal guest with id of `%d` Failed!!!", *virtualGuest.Id)
	}

	if cloudProps.EphemeralDiskSize > 0 {
		err = c.client.AttachSecondDiskToInstance(*virtualGuest.Id, cloudProps.EphemeralDiskSize)
		if err != nil {
			return nil, bosherr.WrapErrorf(err, "Attaching second disk to virtual guest with id of `%d`", *virtualGuest.Id)
		}
	}

	vm, found := c.vmFinder.Find(*virtualGuest.Id)
	if !found {
		return nil, bosherr.Errorf("Creating VM with id of `%d`", *virtualGuest.Id)
	}

	return PostConfigVM(vm, agentID, cloudProps, networks, c.agentOptions, env)
}
