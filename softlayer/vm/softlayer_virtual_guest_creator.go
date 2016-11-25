package vm

import (
	"fmt"
	"net"
	"time"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"
	slhelper "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common/helper"
	bslcstem "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/stemcell"
	sl "github.com/maximilien/softlayer-go/softlayer"

	util "github.com/cloudfoundry/bosh-softlayer-cpi/util"
)

func NewSoftLayerCreator(client sl.Client, agentOptions AgentOptions, featureOptions FeatureOptions, logger boshlog.Logger) VMCreator {
	slhelper.TIMEOUT = 120 * time.Minute
	slhelper.POLLING_INTERVAL = 5 * time.Second

	return &SoftLayerVirtualGuestCreator{
		client: 	 client,
		agentOptions:    agentOptions,
		logger:          logger,
		featureOptions:  featureOptions,
	}
}

func (c *SoftLayerVirtualGuestCreator) Create(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	for _, network := range networks {
		switch network.Type {
		case "dynamic":
			if cloudProps.DisableOsReload || c.FeatureOptions.DisableOsReload {
				return CeateBySoftlayer(agentID, stemcell, cloudProps, networks, env)
			} else {
				if len(network.IP) == 0 {
					return CreateBySoftlayer(agentID, stemcell, cloudProps, networks, env)
				} else {
					return CreateByOSReload(agentID, stemcell, cloudProps, networks, env)
				}

			}
		case "vip":
			return nil, bosherr.Error("SoftLayer Not Support VIP netowrk")
		default:
			continue
		}
	}

	return nil, bosherr.Error("virtual guests must have exactly one dynamic network")
}