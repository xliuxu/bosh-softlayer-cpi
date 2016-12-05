package vm

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	slh "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common/helper"
	slc "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"
	bslcdisk "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/disk"

	"github.com/cloudfoundry/bosh-softlayer-cpi/util"
	"github.com/softlayer/softlayer-go/datatypes"
)

type vm struct {
	id              int
	virtualGuest    datatypes.Virtual_Guest
	client          slc.Client
	sshClient       util.SshClient
	agentEnvService AgentEnvService
	logger          boshlog.Logger
}

func NewSoftLayerInstance(virtualGuest datatypes.Virtual_Guest, client slc.Client, logger boshlog.Logger) VM {
	slh.TIMEOUT = 60 * time.Minute
	slh.POLLING_INTERVAL = 10 * time.Second

	return &vm{
		id: *virtualGuest.Id,
		virtualGuest: virtualGuest,
		client: client,
		sshClient: util.GetSshClient(),
		logger: logger,
	}
}

func (vm *vm) ID() int { return vm.id }

func (vm *vm) GetDataCenterId() int {
	return *vm.virtualGuest.Datacenter.Id
}

func (vm *vm) GetPrimaryIP() string {
	return *vm.virtualGuest.PrimaryIpAddress
}

func (vm *vm) GetPrimaryBackendIP() string {
	return *vm.virtualGuest.PrimaryBackendIpAddress
}

func (vm *vm) GetRootPassword() string {
	passwords := vm.virtualGuest.OperatingSystem.Passwords
	for _, password := range passwords {
		if *password.Username == ROOT_USER_NAME {
			return *password.Password
		}
	}

	return ""
}

func (vm *vm) GetFullyQualifiedDomainName() string {
	return *vm.virtualGuest.FullyQualifiedDomainName
}

func (vm *vm) SetVcapPassword(encryptedPwd string) (err error) {
	command := fmt.Sprintf("usermod -p '%s' vcap", encryptedPwd)
	_, err = vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), command)
	if err != nil {
		return bosherr.WrapError(err, "Shelling out to usermod vcap")
	}
	return
}

func (vm *vm) SetAgentEnvService(agentEnvService AgentEnvService) error {
	if agentEnvService != nil {
		vm.agentEnvService = agentEnvService
	}
	return nil
}

func (vm *vm) ConfigureNetworks(networks Networks) error {
	oldAgentEnv, err := vm.agentEnvService.Fetch()
	if err != nil {
		return bosherr.WrapErrorf(err, "Failed to unmarshal userdata from virutal guest with id: %d.", vm.ID())
	}

	oldAgentEnv.Networks = networks
	err = vm.agentEnvService.Update(oldAgentEnv)
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Configuring network setting on VirtualGuest with id: `%d`", vm.ID()))
	}

	return nil
}

type sshClientWrapper struct {
	client   util.SshClient
	ip       string
	user     string
	password string
}

func (s *sshClientWrapper) Output(command string) ([]byte, error) {
	o, err := s.client.ExecCommand(s.user, s.password, s.ip, command)
	return []byte(o), err
}

func (vm *vm) ConfigureNetworks2(networks Networks) error {
	ubuntu := Ubuntu{
		SoftLayerClient: vm.client.GetSoftLayerClient(),
		SSHClient: &sshClientWrapper{
			client:   vm.sshClient,
			ip:       vm.GetPrimaryBackendIP(),
			user:     ROOT_USER_NAME,
			password: vm.GetRootPassword(),
		},
		SoftLayerFileService: NewSoftlayerFileService(util.GetSshClient(), vm.logger),
	}

	err := ubuntu.ConfigureNetwork(networks, vm)
	if err != nil {
		return bosherr.WrapErrorf(err, "Failed to configure networking for virtual guest with id: %d.", vm.ID())
	}

	return nil
}

func (vm *vm) AttachDisk(disk bslcdisk.Disk) error {
	volume, err := vm.client.GetBlockVolumeDetails(disk.ID(), "")
	if err != nil {
		return bosherr.WrapErrorf(err, "Fetching details of disk with id `%d`", disk.ID())
	}

	until := time.Now().Add(time.Duration(2) * time.Hour)
	succeeded, err := vm.client.AuthorizeHostToVolume(&vm.virtualGuest, disk.ID(), until)
	if err != nil {
		return bosherr.WrapErrorf(err, "Authorizing host with id of `%d` to volume with id of `%d`", vm.ID(), disk.ID())
	}

	if !succeeded {
		return bosherr.Errorf("Authorizing host with id of `%d` to volume with id of `%d` time out !", vm.ID(), disk.ID())
	}

	hasMultiPath, err := vm.hasMulitPathToolBasedOnShellScript()
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Getting multipath information from virtual guest `%d`", vm.ID()))
	}

	deviceName, err := vm.waitForVolumeAttached(volume, hasMultiPath)
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Attaching volume `%d` to virtual guest `%d`", disk.ID(), vm.ID()))
	}
	oldAgentEnv, err := vm.agentEnvService.Fetch()
	if err != nil {
		return bosherr.WrapErrorf(err, "Unmarshaling userdata from virutal guest with id: %d.", vm.ID())
	}

	var newAgentEnv AgentEnv
	if hasMultiPath {
		newAgentEnv = oldAgentEnv.AttachPersistentDisk(strconv.Itoa(disk.ID()), "/dev/mapper/"+deviceName)
	} else {
		newAgentEnv = oldAgentEnv.AttachPersistentDisk(strconv.Itoa(disk.ID()), "/dev/"+deviceName)
	}

	err = vm.agentEnvService.Update(newAgentEnv)
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Configuring userdata on VirtualGuest with id: `%d`", vm.ID()))
	}

	return nil
}

func (vm *vm) DetachDisk(disk bslcdisk.Disk) error {
	volume, err := vm.client.GetBlockVolumeDetails(disk.ID(), "")
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Getting volume detail with id of `%d`", disk.ID()))
	}

	hasMultiPath, err := vm.hasMulitPathToolBasedOnShellScript()
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Getting multipath information from virtual guest `%d`", vm.ID()))
	}

	err = vm.detachVolumeBasedOnShellScript(volume, hasMultiPath)
	if err != nil {
		return bosherr.WrapErrorf(err, "Detaching volume with id of `%d` from virtual guest with id of `%d`", volume.Id, vm.ID())
	}

	until := time.Now().Add(time.Duration(1) * time.Hour)
	succeeded, err := vm.client.DeauthorizeHostToVolume(&vm.virtualGuest, disk.ID(), until)
	if err != nil {
		return bosherr.WrapErrorf(err, "Deauthorizing host with id of `%d` to volume with id of `%d`", vm.ID(), disk.ID())
	}

	if !succeeded {
		return bosherr.Errorf("Deauthorizing host with id of `%d` to volume with id of `%d` time out !", vm.ID(), disk.ID())
	}

	oldAgentEnv, err := vm.agentEnvService.Fetch()
	if err != nil {
		return bosherr.WrapErrorf(err, "Unmarshaling userdata from virutal guest with id of `%d`", vm.ID())
	}

	newAgentEnv := oldAgentEnv.DetachPersistentDisk(strconv.Itoa(disk.ID()))
	err = vm.UpdateAgentEnv(newAgentEnv)
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Configuring userdata on VirtualGuest with id of `%d`", vm.ID()))
	}

	if len(newAgentEnv.Disks.Persistent) == 1 {
		for key, devicePath := range newAgentEnv.Disks.Persistent {
			leftDiskId, err := strconv.Atoi(key)
			if err != nil {
				return bosherr.WrapError(err, fmt.Sprintf("Failed to transfer disk id %s from string to int", key))
			}
			vm.logger.Debug(SOFTLAYER_VM_LOG_TAG, "Left Disk Id %d", leftDiskId)
			vm.logger.Debug(SOFTLAYER_VM_LOG_TAG, "Left Disk device path %s", devicePath)
			volume, err := vm.client.GetBlockVolumeDetails(leftDiskId, "")
			if err != nil {
				return bosherr.WrapError(err, fmt.Sprintf("Failed to fetch disk `%d` and virtual gusest `%d`", disk.ID(), vm.ID()))
			}

			_, err = vm.discoveryOpenIscsiTargetsBasedOnShellScript(volume)
			if err != nil {
				return bosherr.WrapError(err, fmt.Sprintf("Failed to reattach volume `%s` to virtual guest `%d`", key, vm.ID()))
			}

			command := fmt.Sprintf("sleep 5; mount %s-part1 /var/vcap/store", devicePath)
			_, err = vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), command)
			if err != nil {
				return bosherr.WrapError(err, "mount /var/vcap/store")
			}
		}
	}

	return nil
}

func (vm *vm) UpdateAgentEnv(agentEnv AgentEnv) error {
	return vm.agentEnvService.Update(agentEnv)
}

func (vm *vm) waitForVolumeAttached(volume datatypes.Network_Storage, hasMultiPath bool) (string, error) {

	oldDisks, err := vm.getIscsiDeviceNamesBasedOnShellScript(hasMultiPath)
	if err != nil {
		return "", bosherr.WrapError(err, fmt.Sprintf("Failed to get devices names from virtual guest `%d`", vm.ID()))
	}
	if len(oldDisks) > 2 {
		return "", bosherr.Error(fmt.Sprintf("Too manay persistent disks attached to virtual guest `%d`", vm.ID()))
	}

	allowedHost, err := vm.client.GetInstanceAllowedHost(vm.ID())
	if err != nil {
		return "", bosherr.WrapError(err, fmt.Sprintf("Failed to get iscsi host auth from virtual guest `%d`", vm.ID()))
	}

	_, err = vm.backupOpenIscsiConfBasedOnShellScript()
	if err != nil {
		return "", bosherr.WrapError(err, fmt.Sprintf("Failed to backup open iscsi conf files from virtual guest `%d`", vm.ID()))
	}

	_, err = vm.writeOpenIscsiInitiatornameBasedOnShellScript(allowedHost)
	if err != nil {
		return "", bosherr.WrapError(err, fmt.Sprintf("Failed to write open iscsi initiatorname from virtual guest `%d`", vm.ID()))
	}

	_, err = vm.writeOpenIscsiConfBasedOnShellScript(volume, allowedHost.Credential)
	if err != nil {
		return "", bosherr.WrapError(err, fmt.Sprintf("Failed to write open iscsi conf from virtual guest `%d`", vm.ID()))
	}

	_, err = vm.restartOpenIscsiBasedOnShellScript()
	if err != nil {
		return "", bosherr.WrapError(err, fmt.Sprintf("Failed to restart open iscsi from virtual guest `%d`", vm.ID()))
	}

	_, err = vm.discoveryOpenIscsiTargetsBasedOnShellScript(volume)
	if err != nil {
		return "", bosherr.WrapErrorf(err, "Failed to attach volume with id %d to virtual guest with id: %d.", volume.Id, vm.ID())
	}

	var deviceName string
	totalTime := time.Duration(0)
	for totalTime < slh.TIMEOUT {
		newDisks, err := vm.getIscsiDeviceNamesBasedOnShellScript(hasMultiPath)
		if err != nil {
			return "", bosherr.WrapError(err, fmt.Sprintf("Failed to get devices names from virtual guest `%d`", vm.ID()))
		}

		if len(oldDisks) == 0 {
			if len(newDisks) > 0 {
				deviceName = newDisks[0]
				return deviceName, nil
			}
		}

		var included bool
		for _, newDisk := range newDisks {
			for _, oldDisk := range oldDisks {
				if strings.EqualFold(newDisk, oldDisk) {
					included = true
				}
			}
			if !included {
				deviceName = newDisk
			}
			included = false
		}

		if len(deviceName) > 0 {
			return deviceName, nil
		}

		totalTime += slh.POLLING_INTERVAL
		time.Sleep(slh.POLLING_INTERVAL)
	}

	return "", bosherr.Errorf("Failed to attach disk '%d' to virtual guest '%d'", volume.Id, vm.ID())
}

func (vm *vm) hasMulitPathToolBasedOnShellScript() (bool, error) {
	command := fmt.Sprintf("echo `command -v multipath`")
	output, err := vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), command)
	if err != nil {
		return false, err
	}

	if len(output) > 0 && strings.Contains(output, "multipath") {
		return true, nil
	}

	return false, nil
}

func (vm *vm) getIscsiDeviceNamesBasedOnShellScript(hasMultiPath bool) ([]string, error) {
	devices := []string{}

	command1 := fmt.Sprintf("dmsetup ls")
	command2 := fmt.Sprintf("cat /proc/partitions")

	if hasMultiPath {
		result, err := vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), command1)
		if err != nil {
			return devices, err
		}
		if strings.Contains(result, "No devices found") {
			return devices, nil
		}
		vm.logger.Info(SOFTLAYER_VM_LOG_TAG, fmt.Sprintf("Devices on VM %d: %s", vm.ID(), result))
		lines := strings.Split(strings.Trim(result, "\n"), "\n")
		for i := 0; i < len(lines); i++ {
			if match, _ := regexp.MatchString("-part1", lines[i]); !match {
				devices = append(devices, strings.Fields(lines[i])[0])
			}
		}
	} else {
		result, err := vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), command2)
		if err != nil {
			return devices, err
		}

		vm.logger.Info(SOFTLAYER_VM_LOG_TAG, fmt.Sprintf("Devices on VM %d: %s", vm.ID(), result))
		lines := strings.Split(strings.Trim(result, "\n"), "\n")
		for i := 0; i < len(lines); i++ {
			if match, _ := regexp.MatchString("sd[a-z]$", lines[i]); match {
				vals := strings.Fields(lines[i])
				devices = append(devices, vals[len(vals)-1])
			}
		}
	}

	return devices, nil
}

func (vm *vm) backupOpenIscsiConfBasedOnShellScript() (bool, error) {
	command := fmt.Sprintf("cp /etc/iscsi/iscsid.conf{,.save}")
	_, err := vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), command)
	if err != nil {
		return false, bosherr.WrapError(err, "backuping open iscsi conf")
	}

	return true, nil
}

func (vm *vm) restartOpenIscsiBasedOnShellScript() (bool, error) {
	command := fmt.Sprintf("/etc/init.d/open-iscsi restart")
	_, err := vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), command)
	if err != nil {
		return false, bosherr.WrapError(err, "restarting open iscsi")
	}

	return true, nil
}

func (vm *vm) discoveryOpenIscsiTargetsBasedOnShellScript(volume datatypes.Network_Storage) (bool, error) {
	command := fmt.Sprintf("sleep 5; iscsiadm -m discovery -t sendtargets -p %s", volume.ServiceResourceBackendIpAddress)
	_, err := vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), command)
	if err != nil {
		return false, bosherr.WrapError(err, "discoverying open iscsi targets")
	}

	command = "sleep 5; echo `iscsiadm -m node -l`"
	_, err = vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), command)
	if err != nil {
		return false, bosherr.WrapError(err, "login iscsi targets")
	}

	return true, nil
}

func (vm *vm) writeOpenIscsiInitiatornameBasedOnShellScript(allowedHost datatypes.Network_Storage_Allowed_Host) (bool, error) {
	if allowedHost.Name !=nil {
		command := fmt.Sprintf("echo 'InitiatorName=%s' > /etc/iscsi/initiatorname.iscsi", *allowedHost.Name)
		_, err := vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), command)
		if err != nil {
			return false, bosherr.WrapError(err, "Writing to /etc/iscsi/initiatorname.iscsi")
		}
	}

	return true, nil
}

func (vm *vm) writeOpenIscsiConfBasedOnShellScript(volume datatypes.Network_Storage, credential datatypes.Network_Storage_Credential) (bool, error) {
	buffer := bytes.NewBuffer([]byte{})
	t := template.Must(template.New("open_iscsid_conf").Parse(EtcIscsidConfTemplate))
	err := t.Execute(buffer, credential)
	if err != nil {
			return false, bosherr.WrapError(err, "Generating config from template")
	}

	file, err := ioutil.TempFile(os.TempDir(), "iscsid_conf_")
	if err != nil {
		return false, bosherr.WrapError(err, "Generating config from template")
	}

	defer os.Remove(file.Name())

	_, err = file.WriteString(buffer.String())
	if err != nil {
		return false, bosherr.WrapError(err, "Generating config from template")
	}

	if err = vm.sshClient.UploadFile(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), file.Name(), "/etc/iscsi/iscsid.conf"); err != nil {
		return false, bosherr.WrapError(err, "Writing to /etc/iscsi/iscsid.conf")
	}

	return true, nil
}

func (vm *vm) detachVolumeBasedOnShellScript(volume datatypes.Network_Storage, hasMultiPath bool) error {
	// umount /var/vcap/store in case read-only mount
	isMounted, err := vm.isMountPoint("/var/vcap/store")
	if err != nil {
		return bosherr.WrapError(err, "check mount point /var/vcap/store")
	}

	if isMounted {
		step00 := fmt.Sprintf("umount -l /var/vcap/store")
		_, err := vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), step00)
		if err != nil {
			return bosherr.WrapError(err, "umount -l /var/vcap/store")
		}
		vm.logger.Debug(SOFTLAYER_VM_LOG_TAG, "umount -l /var/vcap/store", nil)
	}

	// stop open-iscsi
	step1 := fmt.Sprintf("/etc/init.d/open-iscsi stop")
	_, err = vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), step1)
	if err != nil {
		return bosherr.WrapError(err, "Restarting open iscsi")
	}
	vm.logger.Debug(SOFTLAYER_VM_LOG_TAG, "/etc/init.d/open-iscsi stop", nil)

	// clean up /etc/iscsi/send_targets/
	step2 := fmt.Sprintf("rm -rf /etc/iscsi/send_targets")
	_, err = vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), step2)
	if err != nil {
		return bosherr.WrapError(err, "Removing /etc/iscsi/send_targets")
	}
	vm.logger.Debug(SOFTLAYER_VM_LOG_TAG, "rm -rf /etc/iscsi/send_targets", nil)

	// clean up /etc/iscsi/nodes/
	step3 := fmt.Sprintf("rm -rf /etc/iscsi/nodes")
	_, err = vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), step3)
	if err != nil {
		return bosherr.WrapError(err, "Removing /etc/iscsi/nodes")
	}

	vm.logger.Debug(SOFTLAYER_VM_LOG_TAG, "rm -rf /etc/iscsi/nodes", nil)

	// start open-iscsi
	step4 := fmt.Sprintf("/etc/init.d/open-iscsi start")
	_, err = vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), step4)
	if err != nil {
		return bosherr.WrapError(err, "Restarting open iscsi")
	}
	vm.logger.Debug(SOFTLAYER_VM_LOG_TAG, "/etc/init.d/open-iscsi start", nil)

	if hasMultiPath {
		// restart dm-multipath tool
		step5 := fmt.Sprintf("service multipath-tools restart")
		_, err = vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.GetRootPassword(), vm.GetPrimaryBackendIP(), step5)
		if err != nil {
			return bosherr.WrapError(err, "Restarting Multipath deamon")
		}
		vm.logger.Debug(SOFTLAYER_VM_LOG_TAG, "service multipath-tools restart", nil)
	}

	return nil
}

func (vm *vm) isMountPoint(path string) (bool, error) {
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

func (vm *vm) searchMounts() ([]Mount, error) {
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