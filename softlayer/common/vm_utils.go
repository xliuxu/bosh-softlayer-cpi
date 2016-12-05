package common

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"text/template"
	"time"

	datatypes "github.com/softlayer/softlayer-go/datatypes"
	sl "github.com/softlayer/softlayer-go/sl"

	bslcstem "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/stemcell"

	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshsys "github.com/cloudfoundry/bosh-utils/system"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
)

func CreateDisksSpec(ephemeralDiskSize int) DisksSpec {
	disks := DisksSpec{}
	if ephemeralDiskSize > 0 {
		disks = DisksSpec{
			Ephemeral:  "/dev/xvdc",
			Persistent: nil,
		}
	}

	return disks
}

func TimeStampForTime(now time.Time) string {
	//utilize the constants list in the http://golang.org/src/time/format.go file to get the expect time formats
	return now.Format("20060102-030405-") + strconv.Itoa(int(now.UnixNano()/1e6-now.Unix()*1e3))
}

func GenerateInstanceCreationTemplate(virtualGuest *datatypes.Virtual_Guest, stemcell string, cloudProps VMCloudProperties, networks Networks) *datatypes.Virtual_Guest {
	virtualGuest.Hostname = sl.String(cloudProps.VmNamePrefix)

	virtualGuest.Domain = sl.String(cloudProps.Domain)

	virtualGuest.StartCpus = sl.Int(cloudProps.StartCpus)

	virtualGuest.MaxMemory = sl.Int(cloudProps.StartCpus)

	virtualGuest.Datacenter = &datatypes.Location{
		Name: sl.String(cloudProps.Datacenter.Name),
	}

	virtualGuest.BlockDeviceTemplateGroup = &datatypes.Virtual_Guest_Block_Device_Template_Group{
		GlobalIdentifier: sl.String(stemcell),
	}

	virtualGuest.HourlyBillingFlag = sl.Bool(cloudProps.HourlyBillingFlag)

	virtualGuest.DedicatedAccountHostOnlyFlag = sl.Bool(cloudProps.DedicatedAccountHostOnlyFlag)

	virtualGuest.PrivateNetworkOnlyFlag = sl.Bool(cloudProps.PrivateNetworkOnlyFlag)

	virtualGuest.LocalDiskFlag = sl.Bool(cloudProps.LocalDiskFlag)

	if len(cloudProps.SshKeys) > 0 {
		var securityKeys []datatypes.Security_Ssh_Key
		for _, sshkey := range cloudProps.SshKeys {
			key := datatypes.Security_Ssh_Key{
				Id: sl.Int(sshkey),
			}
			securityKeys = append(securityKeys, key)
		}
		virtualGuest.SshKeys = securityKeys
	}

	virtualGuest.NetworkComponents = []datatypes.Virtual_Guest_Network_Component{
		datatypes.Virtual_Guest_Network_Component{
			MaxSpeed: sl.Int(1000),
		},
	}


	virtualGuest.PrimaryNetworkComponent = &datatypes.Virtual_Guest_Network_Component{
		NetworkVlan: &datatypes.Network_Vlan{
			Id: sl.Int(cloudProps.PublicVlanId),
		},
	}

	virtualGuest.PrimaryBackendNetworkComponent = &datatypes.Virtual_Guest_Network_Component{
		NetworkVlan: &datatypes.Network_Vlan{
			Id: sl.Int(cloudProps.PrivateVlanId),
		},
	}

	return virtualGuest, nil
}

func CreateAgentUserData(agentID string, cloudProps VMCloudProperties, networks Networks, env Environment, agentOptions AgentOptions) AgentEnv {
	agentName := fmt.Sprintf("vm-%s", agentID)
	disks := CreateDisksSpec(cloudProps.EphemeralDiskSize)
	agentEnv := NewAgentEnvForVM(agentID, agentName, networks, disks, env, agentOptions)
	return agentEnv
}

func UpdateDavConfig(config *DavConfig, directorIP string) (err error) {
	url := (*config)["endpoint"].(string)
	mbus, err := ParseMbusURL(url, directorIP)
	if err != nil {
		return bosherr.WrapError(err, "Parsing Mbus URL")
	}

	(*config)["endpoint"] = mbus

	return nil
}

func ParseMbusURL(mbusURL string, primaryBackendIpAddress string) (string, error) {
	parsedURL, err := url.Parse(mbusURL)
	if err != nil {
		return "", bosherr.WrapError(err, "Parsing Mbus URL")
	}
	var username, password, port string
	_, port, _ = net.SplitHostPort(parsedURL.Host)
	userInfo := parsedURL.User
	if userInfo != nil {
		username = userInfo.Username()
		password, _ = userInfo.Password()
		return fmt.Sprintf("%s://%s:%s@%s:%s", parsedURL.Scheme, username, password, primaryBackendIpAddress, port), nil
	}

	return fmt.Sprintf("%s://%s:%s", parsedURL.Scheme, primaryBackendIpAddress, port), nil
}

func UpdateEtcHostsOfBoshInit(record string) (err error) {
	buffer := bytes.NewBuffer([]byte{})
	t := template.Must(template.New("etc-hosts").Parse(ETC_HOSTS_TEMPLATE))

	err = t.Execute(buffer, record)
	if err != nil {
		return bosherr.WrapError(err, "Generating config from template")
	}

	logger := boshlog.NewWriterLogger(boshlog.LevelError, os.Stderr, os.Stderr)
	fs := boshsys.NewOsFileSystem(logger)

	err = fs.WriteFile("/etc/hosts", buffer.Bytes())
	if err != nil {
		return bosherr.WrapError(err, "Writing to /etc/hosts")
	}

	return nil
}

func PostConfigVM(vm VM, agentId string, cloudProps VMCloudProperties, networks Networks, agentOptions AgentOptions, env Environment) (VM, error) {
	if len(cloudProps.BoshIp) {
		UpdateEtcHostsOfBoshInit(fmt.Sprintf("%s  %s", vm.GetPrimaryBackendIP(), vm.GetFullyQualifiedDomainName()))
		mbus, err := ParseMbusURL(agentOptions.Mbus, vm.GetPrimaryBackendIP())
		if err != nil {
			return nil, bosherr.WrapErrorf(err, "Constructing mbus url.")
		}
		agentOptions.Mbus = mbus
	} else {
		mbus, err := ParseMbusURL(agentOptions.Mbus, cloudProps.BoshIp)
		if err != nil {
			return nil, bosherr.WrapErrorf(err, "Constructing mbus url.")
		}
		agentOptions.Mbus = mbus

		switch agentOptions.Blobstore.Provider {
		case BlobstoreTypeDav:
			davConf := DavConfig(agentOptions.Blobstore.Options)
			UpdateDavConfig(&davConf, cloudProps.BoshIp)
		}
	}

	vm.ConfigureNetworks2(networks)

	agentEnv := CreateAgentUserData(agentId, cloudProps, networks, env, agentOptions)

	err := vm.UpdateAgentEnv(agentEnv)
	if err != nil {
		return nil, bosherr.WrapError(err, "Updating VM's agent env")
	}

	if len( agentOptions.VcapPassword) > 0 {
		err = vm.SetVcapPassword(agentOptions.VcapPassword)
		if err != nil {
			return nil, bosherr.WrapError(err, "Updating VM's vcap password")
		}
	}

	return vm, nil
}

const ETC_HOSTS_TEMPLATE = `127.0.0.1 localhost
{{.}}
`
