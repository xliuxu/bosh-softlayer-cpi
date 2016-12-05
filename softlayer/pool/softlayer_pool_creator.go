package pool

import (
	"fmt"
	"net"

	strfmt "github.com/go-openapi/strfmt"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	sl "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"

	"github.com/softlayer/softlayer-go/datatypes"
	"github.com/cloudfoundry/bosh-softlayer-cpi/util"

	operations "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/pool/client/vm"
	bslcstem "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/stemcell"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"

	"github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/pool/models"
)

const SOFTLAYER_POOL_CREATOR_LOG_TAG = "SoftLayerPoolCreator"

type softLayerPoolCreator struct {
	vmFinder               VMFinder
	client                 sl.Client
	agentOptions           AgentOptions
	featureOptions         FeatureOptions
	logger                 boshlog.Logger
}

func NewSoftLayerPoolCreator(vmFinder VMFinder, client sl.Client, agentOptions AgentOptions, featureOptions FeatureOptions, logger boshlog.Logger) VMCreator {
	return &softLayerPoolCreator{
		client:       client,
		agentOptions:          agentOptions,
		logger:                logger,
		vmFinder:              vmFinder,
		featureOptions:        featureOptions,
	}
}

func (c *softLayerPoolCreator) Create(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	for _, network := range networks {
		switch network.Type {
		case "dynamic":
			if len(network.IP) == 0 {
				return c.createFromVMPool(agentID, stemcell, cloudProps, networks, env)
			} else {
				return c.createByOSReload(agentID, stemcell, cloudProps, networks, env)
			}
		case "vip":
			return nil, bosherr.Error("SoftLayer Not Support VIP netowrk")
		default:
			continue
		}
	}
	return nil, bosherr.Error("virtual guests must have exactly one dynamic network")
}

// Private methods
func (c *softLayerPoolCreator) createFromVMPool(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	var err error
	virtualGuestTemplate := GenerateInstanceCreationTemplate(&datatypes.Virtual_Guest{}, stemcell.Uuid(), cloudProps, networks)
	filter := &models.VMFilter{
		CPU:         int32(virtualGuestTemplate.StartCpus),
		MemoryMb:    int32(virtualGuestTemplate.MaxMemory),
		PrivateVlan: int32(virtualGuestTemplate.PrimaryBackendNetworkComponent.NetworkVlan.Id),
		PublicVlan:  int32(virtualGuestTemplate.PrimaryNetworkComponent.NetworkVlan.Id),
		State:       models.StateFree,
	}
	orderVmResp, err := c.client.OrderVMByFilter(operations.NewOrderVMByFilterParams().WithBody(filter))
	if err != nil {
		_, ok := err.(*operations.OrderVMByFilterNotFound)
		if !ok {
			return nil, bosherr.WrapError(err, "Ordering vm from pool")
		} else {
			sl_vm, err := c.createBySoftlayer(agentID, stemcell, cloudProps, networks, env)
			if err != nil {
				return nil, bosherr.WrapError(err, "Creating vm in SoftLayer")
			}
			slPoolVm := &models.VM{
				Cid:         int32(sl_vm.ID()),
				CPU:         int32(virtualGuestTemplate.StartCpus),
				MemoryMb:    int32(virtualGuestTemplate.MaxMemory),
				IP:          strfmt.IPv4(sl_vm.GetPrimaryBackendIP()),
				Hostname:    sl_vm.GetFullyQualifiedDomainName(),
				PrivateVlan: int32(virtualGuestTemplate.PrimaryBackendNetworkComponent.NetworkVlan.Id),
				PublicVlan:  int32(virtualGuestTemplate.PrimaryNetworkComponent.NetworkVlan.Id),
				State:       models.StateUsing,
			}
			_, err = c.client.AddVM(operations.NewAddVMParams().WithBody(slPoolVm))
			if err != nil {
				return nil, bosherr.WrapError(err, "Adding vm into pool")
			}
			c.logger.Info(SOFTLAYER_POOL_CREATOR_LOG_TAG, fmt.Sprintf("Added vm %d to pool successfully", sl_vm.ID()))

			return sl_vm, nil
		}
	}
	var vm *models.VM
	var virtualGuestId int

	vm = orderVmResp.Payload.VM
	virtualGuestId = int((*vm).Cid)

	c.logger.Info(SOFTLAYER_POOL_CREATOR_LOG_TAG, fmt.Sprintf("OS reload on VirtualGuest %d using stemcell %d", virtualGuestId, stemcell.ID()))

	sl_vm_os, err := c.oSReloadVMInPool(virtualGuestId, agentID, stemcell, cloudProps, networks, env)
	if err != nil {
		return nil, bosherr.WrapError(err, "Os reloading vm in SoftLayer")
	}

	using := &models.VMState{
		State: models.StateUsing,
	}
	_, err = c.client.UpdateVMWithState(operations.NewUpdateVMWithStateParams().WithBody(using).WithCid(int32(virtualGuestId)))
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Updating state of vm %d in pool to using", virtualGuestId)
	}

	c.logger.Info(SOFTLAYER_POOL_CREATOR_LOG_TAG, fmt.Sprintf("vm %d using stemcell %d os reload completed", virtualGuestId, stemcell.ID()))

	return sl_vm_os, nil
}

func (c *softLayerPoolCreator) createBySoftlayer(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	var virtualGuest datatypes.Virtual_Guest
	var err error

	virtualGuestTemplate := GenerateInstanceCreationTemplate(&datatypes.Virtual_Guest{},stemcell.Uuid(), cloudProps, networks)
	virtualGuest, err = c.client.CreateInstance(virtualGuestTemplate)
	if err != nil {
		return nil, bosherr.WrapError(err, "Creating virtual guest from softlayer")
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

func (c *softLayerPoolCreator) createByOSReload(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
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
				return nil, bosherr.WrapErrorf(err, "Finding virtual guest with ip address of `%s`", network.IP)
			}
		case "manual":
			continue
		default:
			return nil, bosherr.Errorf("unexpected network type: %s", network.Type)
		}
	}

	err = c.client.ReloadInstance(*virtualGuest.Id, stemcell.ID())
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "OS Reloading virutal guest with id of `%d`", *virtualGuest.Id)
	}

	if cloudProps.EphemeralDiskSize > 0 {
		err = c.client.AttachSecondDiskToInstance(*virtualGuest.Id, cloudProps.EphemeralDiskSize)
		if err != nil {
			return nil, bosherr.WrapErrorf(err, "Attaching second disk to virtual guest with id of `%d`", *virtualGuest.Id)
		}
	}

	vm, found := c.vmFinder.Find(virtualGuest.Id)
	if err != nil || !found {
		return nil, bosherr.Errorf("Creating VM with id of `%d`", *virtualGuest.Id)
	}

	return PostConfigVM(vm, agentID, cloudProps, networks, c.agentOptions, env)
}

func (c *softLayerPoolCreator) oSReloadVMInPool(cid int, agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	err := c.client.ReloadInstance(cid, stemcell.ID())
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "OS Reloading virutal guest with id of `%d`", cid)
	}

	if cloudProps.EphemeralDiskSize > 0 {
		err = c.client.AttachSecondDiskToInstance(cid, cloudProps.EphemeralDiskSize)
		if err != nil {
			return nil, bosherr.WrapErrorf(err, "Attaching second disk to virtual guest with id of `%d`", cid)
		}
	}

	vm, found := c.vmFinder.Find(cid)
	if err != nil || !found {
		return nil, bosherr.Errorf("Creating VM with id of `%d`", cid)
	}

	return PostConfigVM(vm, agentID, cloudProps, networks, c.agentOptions, env)
}
