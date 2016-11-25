package vm

import (
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/hardware"
	sl "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"

	"github.com/cloudfoundry/bosh-softlayer-cpi/util"
)

type softLayerFinder struct {
	client 		       sl.Client
	agentEnvServiceFactory AgentEnvServiceFactory
	logger                 boshlog.Logger
}

func NewSoftLayerFinder(client *sl.Client, agentEnvServiceFactory AgentEnvServiceFactory, logger boshlog.Logger) VMFinder {
	return &softLayerFinder{
		client:			client,
		agentEnvServiceFactory: agentEnvServiceFactory,
		logger:                 logger,
	}
}

func (f *softLayerFinder) Find(vmID int) (VM, bool, error) {
	var vm VM

	virtualGuest, err := f.client.GetVirtualGuestObject(vmID)
	if err != nil {
		hardware, err := f.client.GetHardwareObject(vmID)
		if err != nil {
			return nil, false, bosherr.Errorf("Failed to find VM or Baremetal %d", vmID)
		}
		vm = NewSoftLayerHardware(hardware, f.client, util.GetSshClient(), f.logger)
	} else {
		vm = NewSoftLayerVirtualGuest(virtualGuest, f.client, util.GetSshClient(), f.logger)
	}

	softlayerFileService := NewSoftlayerFileService(util.GetSshClient(), f.logger)
	agentEnvService := f.agentEnvServiceFactory.New(vm, softlayerFileService)
	vm.SetAgentEnvService(agentEnvService)
	return vm, true, nil
}
