package hardware

import (
	"fmt"
	"strconv"
	"strings"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	slc "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"

	"github.com/cloudfoundry/bosh-softlayer-cpi/api"
	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"
	"github.com/cloudfoundry/bosh-softlayer-cpi/util"

	bslcdisk "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/disk"
	bslcstem "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/stemcell"
	datatypes "github.com/softlayer/softlayer-go/datatypes"
)

type softLayerHardware struct {
	id              int
	hardware        datatypes.Hardware
	client          slc.Client
	sshClient       util.SshClient
	agentEnvService AgentEnvService
	logger          boshlog.Logger
}

func NewSoftLayerHardware(hardware datatypes.Hardware, client slc.Client, logger boshlog.Logger) VM {
	return &softLayerHardware{
		id: *hardware.Id,
		hardware: hardware,
		client: client,
		sshClient:   util.GetSshClient(),
		logger: logger,
	}
}

func (vm *softLayerHardware) ID() int { return vm.id }

func (vm *softLayerHardware) GetDataCenterId() int {
	return *vm.hardware.Datacenter.Id
}

func (vm *softLayerHardware) GetPrimaryIP() string {
	return *vm.hardware.PrimaryIpAddress
}

func (vm *softLayerHardware) GetPrimaryBackendIP() string {
	return *vm.hardware.PrimaryBackendIpAddress
}

func (vm *softLayerHardware) GetRootPassword() string {
	passwords := vm.hardware.OperatingSystem.Passwords
	for _, password := range passwords {
		if *password.Username == ROOT_USER_NAME {
			return *password.Password
		}
	}
	return ""
}

func (vm *softLayerHardware) GetFullyQualifiedDomainName() string {
	return *vm.hardware.FullyQualifiedDomainName
}

func (vm *softLayerHardware) SetAgentEnvService(agentEnvService AgentEnvService) error {
	if agentEnvService != nil {
		vm.agentEnvService = agentEnvService
	}
	return nil
}

func (vm *softLayerHardware) SetVcapPassword(encryptedPwd string) (err error) {
	command := fmt.Sprintf("usermod -p '%s' vcap", encryptedPwd)
	_, err = vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), command)
	if err != nil {
		return bosherr.WrapError(err, "Shelling out to usermod vcap")
	}
	return
}

func (vm *softLayerHardware) Delete(agentID string) error {
	updateStateResponse, err := vm.client.UpdateState(strconv.Itoa(vm.ID()), "bm.state.deleted")
	if err != nil || updateStateResponse.Status != 200 {
		return bosherr.WrapError(err, "Failed to call bms to delete baremetal")
	}

	command := "rm -f /var/vcap/bosh/*.json ; sv stop agent"
	_, err = vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), command)
	return err
}

func (vm *softLayerHardware) Reboot() error {
	return api.NotSupportedError{}
}

func (vm *softLayerHardware) ReloadOS(stemcell bslcstem.Stemcell) error {
	return api.NotSupportedError{}
}

func (vm *softLayerHardware) ReloadOSForBaremetal(stemcell string, netbootImage string) error {
	updateStateResponse, err := vm.client.UpdateState(strconv.Itoa(vm.ID()), "bm.state.new")
	if err != nil || updateStateResponse.Status != 200 {
		return bosherr.WrapError(err, "Failed to call bms to update state of baremetal")
	}

	hardwareId, err := vm.client.ProvisionBaremetal(strconv.Itoa(vm.ID()), stemcell, netbootImage)
	if err != nil {
		return bosherr.WrapError(err, "Provision baremetal error")
	}

	if hardwareId == vm.ID() {
		return nil
	}

	return bosherr.Errorf("Failed to do os_reload against baremetal with id: %d", vm.ID())
}

func (vm *softLayerHardware) SetMetadata(vmMetadata VMMetadata) error {
	vm.logger.Debug(SOFTLAYER_HARDWARE_LOG_TAG, "set_vm_metadata not support for baremetal")
	return nil
}

func (vm *softLayerHardware) ConfigureNetworks(networks Networks) error {
	oldAgentEnv, err := vm.agentEnvService.Fetch()
	if err != nil {
		return bosherr.WrapErrorf(err, "Failed to unmarshal userdata from hardware with id: %d.", vm.ID())
	}

	oldAgentEnv.Networks = networks
	err = vm.agentEnvService.Update(oldAgentEnv)
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Configuring network setting on hardware with id: `%d`", vm.ID()))
	}

	return nil
}

func (vm *softLayerHardware) ConfigureNetworks2(networks Networks) error {
	return api.NotSupportedError{}
}

func (vm *softLayerHardware) AttachDisk(disk bslcdisk.Disk) error {
	return api.NotSupportedError{}
}

func (vm *softLayerHardware) DetachDisk(disk bslcdisk.Disk) error {
	return api.NotSupportedError{}
}

func (vm *softLayerHardware) UpdateAgentEnv(agentEnv AgentEnv) error {
	return vm.agentEnvService.Update(agentEnv)
}

func (vm *softLayerHardware) isMountPoint(path string) (bool, error) {
	mounts, err := vm.searchMounts()
	if err != nil {
		return false, bosherr.WrapError(err, "Searching mounts")
	}

	for _, mount := range mounts {
		if mount.MountPoint == path {
			return true, nil
		}
	}

	return false, nil
}

func (vm *softLayerHardware) searchMounts() ([]Mount, error) {
	var mounts []Mount
	stdout, err := vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), "mount")
	if err != nil {
		return mounts, bosherr.WrapError(err, "Running mount")
	}

	// e.g. '/dev/sda on /boot type ext2 (rw)'
	for _, mountEntry := range strings.Split(stdout, "\n") {
		if mountEntry == "" {
			continue
		}

		mountFields := strings.Fields(mountEntry)

		mounts = append(mounts, Mount{
			PartitionPath: mountFields[0],
			MountPoint:    mountFields[2],
		})

	}

	return mounts, nil
}