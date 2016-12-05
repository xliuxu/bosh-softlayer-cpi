package vm

import (
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"
	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/hardware"

	"github.com/cloudfoundry/bosh-softlayer-cpi/util"

	slcpi "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"
)

type vmFinder struct {
	client                 slcpi.Client
	agentEnvServiceFactory AgentEnvServiceFactory
	logger                 boshlog.Logger
}

func NewVMFinder(client *slcpi.Client, agentEnvServiceFactory AgentEnvServiceFactory, logger boshlog.Logger) VMFinder {
	return &vmFinder{
		client:			client,
		agentEnvServiceFactory: agentEnvServiceFactory,
		logger:                 logger,
	}
}

func (f *vmFinder) Find(vmID int) (VM, error) {
	var vm VM
	instance, err := f.client.GetInstance(vmID, "")
	if err != nil {
		hardware, err := f.client.GetHardware(vmID, "")
		if err != nil {
			return nil, err
		}
		vm = NewSoftLayerHardware(hardware, f.client, f.logger)
	} else {
		vm = NewSoftLayerInstance(instance, f.client, f.logger)
	}

	softlayerFileService := NewSoftlayerFileService(util.GetSshClient(), f.logger)
	agentEnvService := f.agentEnvServiceFactory.New(vm, softlayerFileService)
	vm.SetAgentEnvService(agentEnvService)

	return vm, nil
}
