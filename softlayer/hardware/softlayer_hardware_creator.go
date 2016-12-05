package hardware

import (
	"fmt"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"
	bslcstem "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/stemcell"
	slc "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"
)

type baremetalCreator struct {
	client       slc.Client
	vmFinder     VMFinder
	agentOptions AgentOptions
	logger       boshlog.Logger
}

func NewBaremetalCreator(client slc.Client, vmFinder VMFinder, agentOptions AgentOptions, logger boshlog.Logger) VMCreator {
	return &baremetalCreator{
		client: 	 client,
		agentOptions:    agentOptions,
		logger:          logger,
	}
}

func (c *baremetalCreator) Create(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	for _, network := range networks {
		switch network.Type {
		case "dynamic":
			if len(network.IP) == 0 {
				return c.createByBaremetal(agentID, stemcell, cloudProps, networks, env)
			} else {
				return c.createByOSReload(agentID, stemcell, cloudProps, networks, env)
			}
		case "manual":
			return nil, bosherr.Error("Manual networking is not currently supported")
		case "vip":
			return nil, bosherr.Error("SoftLayer Not Support VIP netowrk")
		default:
			return nil, bosherr.Errorf("Softlayer Not Support This Kind Of Network: %s", network.Type)
		}
	}

	return nil, nil
}

func (c *baremetalCreator) createByBaremetal(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	hardwareId, err := c.client.ProvisionBaremetal(cloudProps.VmNamePrefix, cloudProps.BaremetalStemcell, cloudProps.BaremetalNetbootImage)
	if err != nil {
		return nil, bosherr.WrapError(err, "Creating hardware error")
	}

	hardware, found := c.vmFinder.Find(hardwareId)
	if !found {
		return nil, bosherr.WrapErrorf(err, "Finding hardware with id: %d", hardwareId)
	}

	mbus, err := ParseMbusURL(c.agentOptions.Mbus, cloudProps.BoshIp)
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Constructing mbus url")
	}
	c.agentOptions.Mbus = mbus

	switch c.agentOptions.Blobstore.Provider {
	case BlobstoreTypeDav:
		davConf := DavConfig(c.agentOptions.Blobstore.Options)
		UpdateDavConfig(&davConf, cloudProps.BoshIp)
	}

	agentEnv := CreateAgentUserData(agentID, cloudProps, networks, env, c.agentOptions)

	err = hardware.UpdateAgentEnv(agentEnv)
	if err != nil {
		return nil, bosherr.WrapError(err, "Updating VM's agent env")
	}

	if len(c.agentOptions.VcapPassword) > 0 {
		err = hardware.SetVcapPassword(c.agentOptions.VcapPassword)
		if err != nil {
			return nil, bosherr.WrapError(err, "Updating VM's vcap password")
		}
	}

	return hardware, nil
}

func (c *baremetalCreator) createByOSReload(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	if len(cloudProps.BaremetalStemcell) == 0 {
		return nil, bosherr.Error("No stemcell provided to do os_reload.")
	}

	hardware, err :=c.client.GetHardwareObjectByIpAddress(networks.First().IP)
	if err != nil || hardware.Id == 0 {
		return nil, bosherr.WrapErrorf(err, "Could not find hardware by ip address: %s", networks.First().IP)
	}

	c.logger.Info(SOFTLAYER_VM_CREATOR_LOG_TAG, fmt.Sprintf("OS reload on Hardware %d using stemcell %d", hardware.Id, stemcell.ID()))

	vm, found := c.vmFinder.Find(hardware.Id)
	if err != nil || !found {
		return nil, bosherr.WrapErrorf(err, "Finding hardware with id: %d", hardware.Id)
	}

	err = c.client.ReloadBaremetal(hardware.Id, cloudProps.BaremetalStemcell, cloudProps.BaremetalNetbootImage)
	if err != nil {
		return nil, bosherr.WrapError(err, "Reloading hardware")
	}

	vm, found, err = c.vmFinder.Find(hardware.Id)
	if err != nil || !found {
		return nil, bosherr.WrapErrorf(err, "Finding hardware with id: %d.", vm.ID())
	}

	// Update mbus url setting
	mbus, err := ParseMbusURL(c.agentOptions.Mbus, cloudProps.BoshIp)
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Constructing mbus url.")
	}
	c.agentOptions.Mbus = mbus
	// Update blobstore setting
	switch c.agentOptions.Blobstore.Provider {
	case BlobstoreTypeDav:
		davConf := DavConfig(c.agentOptions.Blobstore.Options)
		UpdateDavConfig(&davConf, cloudProps.BoshIp)
	}

	agentEnv := CreateAgentUserData(agentID, cloudProps, networks, env, c.agentOptions)
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Creating agent env for hardware with id: %d.", vm.ID())
	}

	err = vm.UpdateAgentEnv(agentEnv)
	if err != nil {
		return nil, bosherr.WrapError(err, "Updating VM's agent env")
	}

	if len(c.agentOptions.VcapPassword) > 0 {
		err = vm.SetVcapPassword(c.agentOptions.VcapPassword)
		if err != nil {
			return nil, bosherr.WrapError(err, "Updating VM's vcap password")
		}
	}

	return vm, nil
}
