package softlayer

import (
	"fmt"
	"net/http"
	"time"
	"strings"
	"strconv"

	slclient "github.com/maximilien/softlayer-go/client"
	sldatatypes "github.com/maximilien/softlayer-go/data_types"

	bmsclient "github.com/cloudfoundry-community/bosh-softlayer-tools/clients"

	vpsclient "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/pool/client/vm"

	bslstem "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/stemcell"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/pool/client/vm"
	"github.com/go-openapi/runtime"
)

var (
	TIMEOUT             time.Duration
	POLLING_INTERVAL    time.Duration
)

//go:generate counterfeiter -o repfakes/fake_client_factory.go . ClientFactory
type ClientFactory interface {
	CreateClient() Client
}

type clientFactory struct {
	slClient 	*slclient.SoftLayerClient
	bmsClient       *bmsclient.BmpClient
	vpsClient       *vpsclient.Client
}

func NewClientFactory(slClient *slclient.SoftLayerClient, bmsClient *bmsclient.BmpClient, vpsClient *vpsclient.Client) ClientFactory {
	return &clientFactory{slClient, bmsClient, vpsClient}
}

func (factory *clientFactory) CreateClient() Client {
	return NewClient(factory.slClient, factory.bmsClient, factory.vpsClient)
}

type SoftLayerClient interface {
	CreateVirtualGuest(sldatatypes.SoftLayer_Virtual_Guest_Template) (sldatatypes.SoftLayer_Virtual_Guest, error)
	CancelVirtualGuest(cid int) (bool, error)
	ReloadVirtualGuest(cid int, bslstem.Stemcell) error
	EditVirtualGuest(cid int, sldatatypes.SoftLayer_Virtual_Guest) (bool, error)

	GetVirtualGuestObject(cid int) (sldatatypes.SoftLayer_Virtual_Guest, error)
	GetHardwareObject(cid int) (sldatatypes.SoftLayer_Hardware, error)

	WaitForVirtualGuestLastCompleteTransaction(cid int, targetTransaction string) error
	WaitForVirtualGuestPowerState(cid int, targetState string) error
	WaitForVirtualGuestToHaveRunningTransaction(cid int) error
	WaitForVirtualGuestToHaveNoRunningTransaction(cid int) error
}

type BmsClient interface {
	OrderHardware(vmNamePrefix string, baremetal_stemcell string, netboot_image string)
	ReloadHardware(cid int, stemcell string, netbootImage string)
	CancelHardware(cid int)
}

type SoftLayerPoolClient interface {
	AddVM(params *AddVMParams) (*AddVMOK, error)
	DeleteVM(params *DeleteVMParams) (*DeleteVMNoContent, error)
	FindVmsByDeployment(params *FindVmsByDeploymentParams) (*FindVmsByDeploymentOK, error)
	FindVmsByFilters(params *FindVmsByFiltersParams) (*FindVmsByFiltersOK, error)
	FindVmsByStates(params *FindVmsByStatesParams) (*FindVmsByStatesOK, error)
	GetVMByCid(params *GetVMByCidParams) (*GetVMByCidOK, error)
	ListVM(params *ListVMParams) (*ListVMOK, error)
	OrderVMByFilter(params *OrderVMByFilterParams) (*OrderVMByFilterOK, error)
	UpdateVM(params *UpdateVMParams) (*UpdateVMOK, error)
	UpdateVMWithState(params *UpdateVMWithStateParams) (*UpdateVMWithStateOK, error)
	SetTransport(transport runtime.ClientTransport)
}

//go:generate counterfeiter -o repfakes/fake_client.go . Client
type Client interface {
	SoftLayerPoolClient
	BmsClient
	SoftLayerClient
}

//go:generate counterfeiter -o repfakes/fake_sim_client.go . SimClient
type SimClient interface {
	Client
	Reset() error
}

type client struct {
	slClient         *slclient.SoftLayerClient
	bmsClient        *bmsclient.BmpClient
	vpsClient        *vpsclient.Client
}

func NewClient(slClient *slclient.SoftLayerClient, bmsClient *bmsclient.BmpClient, vpsClient *vpsclient.Client) Client {
	return &client{
		slClient:       slClient,
		bmsClient:      bmsClient,
		vpsClient:	vpsClient,
	}
}

func (c *client) SetStateClient(stateClient *http.Client) {

}

func (c *client) StateClientTimeout() time.Duration {
	return nil
}

func (c *client) CreateVirtualGuest(vg_tmpl sldatatypes.SoftLayer_Virtual_Guest_Template) (sldatatypes.SoftLayer_Virtual_Guest, error) {
	virtualGuestService, _ := c.slClient.GetSoftLayer_Virtual_Guest_Service()
	virtualGuest, err := virtualGuestService.CreateObject(vg_tmpl)
	if err != nil {
		return sldatatypes.SoftLayer_Virtual_Guest{}, bosherr.WrapError(err, "Creating VirtualGuest from SoftLayer client")
	}

	err = c.WaitForVirtualGuestLastCompleteTransaction(virtualGuest.Id, "Service Setup")
	if err != nil {
		return sldatatypes.SoftLayer_Virtual_Guest{}, bosherr.WrapError(err, "Waitting for virtual guest `Service Setup`")
	}

	err = c.WaitForVirtualGuestPowerState(virtualGuest.Id, "RUNNING")
	if err != nil {
		return bosherr.WrapErrorf(err, "Waiting for VirtualGuest `%d`", virtualGuest.Id)
	}

	return virtualGuest, nil
}

func (c *client) CancelVirtualGuest(cid int) (bool, error) {
	err := c.WaitForVirtualGuestToHaveNoRunningTransaction(cid)
	if err != nil {
		return false, bosherr.WrapError(err, fmt.Sprintf("Waiting for VirtualGuest %d to have no pending transactions before cancel virtual guest", cid))
	}

	virtualGuestService, _ := c.slClient.GetSoftLayer_Virtual_Guest_Service()
	result, err := virtualGuestService.DeleteObject(cid)
	if err != nil {
		return false, bosherr.WrapError(err, "Cancelling VirtualGuest from SoftLayer client")
	}

	return result, nil
}

func (c *client) ReloadVirtualGuest(cid int, stemcell bslstem.Stemcell) error {
	err := c.WaitForVirtualGuestToHaveNoRunningTransaction(cid)
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Waiting for VirtualGuest %d to have no pending transactions before os reload", cid))
	}

	virtualGuestService, _ := c.slClient.GetSoftLayer_Virtual_Guest_Service()
	reload_OS_Config := sldatatypes.Image_Template_Config{
		ImageTemplateId: strconv.Itoa(stemcell.ID()),
	}
	err = virtualGuestService.ReloadOperatingSystem(cid, reload_OS_Config)
	if err != nil {
		return bosherr.WrapError(err, "Failed to reload OS on the specified VirtualGuest from SoftLayer client")
	}

	return c.postCheckActiveTransactionsForOSReload(cid)
}

func (c *client) EditVirtualGuest(cid int, virtualGuest sldatatypes.SoftLayer_Virtual_Guest) (bool, error) {
	virtualGuestService, _ := c.slClient.GetSoftLayer_Virtual_Guest_Service()
	result, err := virtualGuestService.EditObject(cid, virtualGuest)
	if err != nil {
		return false, bosherr.WrapError(err, "Creating VirtualGuest from SoftLayer client")
	}
	return result, nil
}

func (c *client) WaitForVirtualGuestLastCompleteTransaction(cid int, targetTransaction string) error {
	virtualGuestService, _ := c.slClient.GetSoftLayer_Virtual_Guest_Service()
	totalTime := time.Duration(0)
	for totalTime < TIMEOUT {
		lastTransaction, err := virtualGuestService.GetLastTransaction(cid)
		if err != nil {
			return bosherr.WrapErrorf(err, "Getting Last Complete Transaction for virtual guest with ID '%d'", cid)
		}

		if strings.Contains(lastTransaction.TransactionGroup.Name, targetTransaction) && strings.Contains(lastTransaction.TransactionStatus.FriendlyName, "Complete") {
			return nil
		}

		totalTime += POLLING_INTERVAL
		time.Sleep(POLLING_INTERVAL)
	}

	return bosherr.Errorf("Waiting for virtual guest with ID '%d' to have last transaction '%s'", cid, targetTransaction)
}

func (c *client) WaitForVirtualGuestPowerState(cid int, targetState string) error {
	virtualGuestService, _ :=c.slClient.GetSoftLayer_Virtual_Guest_Service()
	totalTime := time.Duration(0)
	for totalTime < TIMEOUT {
		vgPowerState, err := virtualGuestService.GetPowerState(cid)
		if err != nil {
			return bosherr.WrapErrorf(err, "Getting Power State for virtual guest with ID '%d'", cid)
		}

		if strings.Contains(vgPowerState.KeyName, targetState) {
			return nil
		}

		totalTime += POLLING_INTERVAL
		time.Sleep(POLLING_INTERVAL)
	}

	return bosherr.Errorf("Waiting for virtual guest with ID '%d' to have be in state '%s'", cid, targetState)
}

func (c *client) WaitForVirtualGuestToHaveRunningTransaction(cid int) error {
	virtualGuestService, _ := c.slClient.GetSoftLayer_Virtual_Guest_Service()

	totalTime := time.Duration(0)
	for totalTime < TIMEOUT {
		activeTransactions, err := virtualGuestService.GetActiveTransactions(cid)
		if err != nil {
			return bosherr.WrapErrorf(err, "Getting active transaction against virtual guest %d", cid)
		}

		if len(activeTransactions) > 0 {
			return nil
		}

		totalTime += POLLING_INTERVAL
		time.Sleep(POLLING_INTERVAL)
	}

	return bosherr.Errorf("Time Out !!! Waiting for virtual guest with ID '%d' to have no active transactions TIME OUT!", cid)
}

func (c *client) WaitForVirtualGuestToHaveNoRunningTransaction(cid int) error {
	virtualGuestService, _ := c.slClient.GetSoftLayer_Virtual_Guest_Service()

	totalTime := time.Duration(0)
	for totalTime < TIMEOUT {
		activeTransactions, err := virtualGuestService.GetActiveTransactions(cid)
		if err != nil {
			return bosherr.WrapErrorf(err, "Getting active transaction against virtual guest %d", cid)
		}

		if len(activeTransactions) == 0 {
			return nil
		}

		totalTime += POLLING_INTERVAL
		time.Sleep(POLLING_INTERVAL)
	}

	return bosherr.Errorf("Waiting for virtual guest with ID '%d' to have no active transactions TIME OUT!", cid)
}

func (c *client) GetVirtualGuestObject(cid int) (sldatatypes.SoftLayer_Virtual_Guest, error) {
	virtualGuestService, _ := c.slClient.GetSoftLayer_Virtual_Guest_Service()
	virtualGuest, err := virtualGuestService.GetObject(cid)
	if err != nil {
		return sldatatypes.SoftLayer_Virtual_Guest{}, bosherr.WrapErrorf(err, "Getting object of virtual guest with id: %d", cid)
	}
	return virtualGuest, nil
}

func (c *client) GetHardwareObject(cid int) (sldatatypes.SoftLayer_Hardware, error) {
	hardwareService, _ := c.slClient.GetSoftLayer_Hardware_Service()
	hardware, err := hardwareService.GetObject(cid)
	if err != nil {
		return sldatatypes.SoftLayer_Hardware{}, bosherr.WrapErrorf(err, "Getting object of hardware with id: %d", cid)
	}
	return hardware, nil
}

// private method
func (c *client) postCheckActiveTransactionsForOSReload(cid int) error {
	err := c.WaitForVirtualGuestToHaveRunningTransaction(cid)
	if err != nil {
		return bosherr.Error(fmt.Sprintf("Waiting for OS Reload transaction to start"))
	}

	err = c.WaitForVirtualGuestPowerState(cid, "RUNNING")
	if err != nil {
		return bosherr.WrapError(err, "Waiting for virtual guest running after OS Reload")
	}
	return nil
}
