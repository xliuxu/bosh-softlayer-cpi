package action

import (
	sl "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"
	slhw "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/hardware"
	slpool "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/pool"
	slvm "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/vm"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
)

//go:generate counterfeiter -o fakes/fake_creator_provider.go . CreatorProvider
type CreatorProvider interface {
	Get(name string) VMCreator
}

//go:generate counterfeiter -o fakes/fake_deleter_provider.go . DeleterProvider
type DeleterProvider interface {
	Get(name string) VMDeleter
}

type creatorProvider struct {
	creators map[string]VMCreator
}

type deleterProvider struct {
	deleters map[string]VMDeleter
}

func NewCreatorProvider(client sl.Client, options ConcreteFactoryOptions, logger boshlog.Logger) CreatorProvider {
	agentEnvServiceFactory := NewSoftLayerAgentEnvServiceFactory(options.AgentEnvService, options.Registry, logger)

	vmFinder := slvm.NewVMFinder(
		client,
		agentEnvServiceFactory,
		logger,
	)

	virtualGuestCreator := slvm.NewSoftLayerCreator(
		client,
		vmFinder,
		options.Agent,
		options.Softlayer.FeatureOptions,
		logger,
	)

	baremetalCreator := slhw.NewBaremetalCreator(
		client,
		vmFinder,
		options.Agent,
		logger,
	)

	poolCreator := slpool.NewSoftLayerPoolCreator(
		vmFinder,
		client,
		options.Agent,
		options.Softlayer.FeatureOptions,
		logger,
	)

	return creatorProvider{
		creators: map[string]VMCreator{
			"virtualguest": virtualGuestCreator,
			"baremetal":    baremetalCreator,
			"pool":         poolCreator,
		},
	}
}

func (p creatorProvider) Get(name string) VMCreator {
	return p.creators[name]
}

func NewDeleterProvider(client sl.Client, logger boshlog.Logger) DeleterProvider {
	virtualGuestDeleter := slvm.NewSoftLayerVMDeleter(
		client,
		logger,
	)

	poolDeleter := slpool.NewSoftLayerPoolDeleter(
		client,
		logger,
	)

	return deleterProvider{
		deleters: map[string]VMDeleter{
			"virtualguest": virtualGuestDeleter,
			"pool":         poolDeleter,
		},
	}
}

func (p deleterProvider) Get(name string) VMDeleter {
	return p.deleters[name]
}
