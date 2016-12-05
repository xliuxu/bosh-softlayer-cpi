package softlayer

import (
	"strings"
	"net/http"
	"time"
	"strconv"
	"math"
	"sort"

	"github.com/softlayer/softlayer-go/datatypes"
	"github.com/softlayer/softlayer-go/filter"
	"github.com/softlayer/softlayer-go/services"
	"github.com/softlayer/softlayer-go/session"
	"github.com/softlayer/softlayer-go/sl"

	"github.com/go-openapi/runtime"

	bmscli "github.com/cloudfoundry-community/bosh-softlayer-tools/clients"
	vpscli "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/pool/client/vm"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/pool/client/vm"
)

const (
	INSTANCE_DEFAULT_MASK = "id, globalIdentifier, hostname, hourlyBillingFlag, domain, fullyQualifiedDomainName, status.name, " +
	"powerState.name, activeTransaction, datacenter.name, account.id, " +
	"maxCpu, maxMemory, primaryIpAddress, primaryBackendIpAddress, " +
	"privateNetworkOnlyFlag, dedicatedAccountHostOnlyFlag, createDate, modifyDate, " +
	"billingItem[orderItem.order.userRecord.username, recurringFee], notes, tagReferences.tag.name"

	VOLUME_DEFAULT_MASK = "id,username,lunId,capacityGb,bytesUsed,serviceResource.datacenter.name,serviceResourceBackendIpAddress,activeTransactionCount,billingItem.orderItem.order[id,userRecord.username]"

	VOLUME_DETAIL_MASK  = "id,username,password,capacityGb,snapshotCapacityGb,parentVolume.snapshotSizeBytes,storageType.keyName," +
	"serviceResource.datacenter.name,serviceResourceBackendIpAddress,iops,lunId,activeTransactionCount," +
	"activeTransactions.transactionStatus.friendlyName,replicationPartnerCount,replicationStatus," +
	"replicationPartners[id,username,serviceResourceBackendIpAddress,serviceResource.datacenter.name,replicationSchedule.type.keyname]"

	IMAGE_DEFAULT_MASK = "id, name, imageType, accountId"
	IMAGE_DETAIL_MASK = "id,globalIdentifier,name,datacenter.name,status.name,transaction.transactionStatus.name,accountId,publicFlag,imageType,flexImageFlag,note,createDate,blockDevicesDiskSpaceTotal,children[blockDevicesDiskSpaceTotal,datacenter.name]"


	EPHEMERAL_DISK_CATEGORY_CODE = "guest_disk1"
	UPGRADE_VIRTUAL_SERVER_ORDER_TYPE = "SoftLayer_Container_Product_Order_Virtual_Guest_Upgrade"

	NETWORK_PERFORMANCE_STORAGE_PACKAGE_ID = 222
)

//go:generate counterfeiter -o repfakes/fake_client_factory.go . ClientFactory
type ClientFactory interface {
	CreateClient() Client
}

type clientFactory struct {
	slClient 	*softlayerClientManager
	bmsClient       *bmscli.BmpClient
	vpsClient       *vpscli.Client
}

func NewClientFactory(slClient *softlayerClientManager, bmsClient *bmscli.BmpClient, vpsClient *vpscli.Client) ClientFactory {
	return &clientFactory{slClient, bmsClient, vpsClient}
}

func (factory *clientFactory) CreateClient() Client {
	return NewClient(factory.slClient, factory.bmsClient, factory.vpsClient)
}

type softlayerClientManager struct {
	VirtualGuestService services.Virtual_Guest
	AccountService      services.Account
	PackageService      services.Product_Package
	OrderService        services.Product_Order
	StorageService      services.Network_Storage
	BillingService      services.Billing_Item
	LocationService     services.Location_Datacenter
	ImageService        services.Virtual_Guest_Block_Device_Template_Group
}

func NewSoftLayerClientManager(session *session.Session) *softlayerClientManager {
	return &softlayerClientManager{
		services.GetVirtualGuestService(session),
		services.GetAccountService(session),
		services.GetProductPackageService(session),
		services.GetProductOrderService(session),
	}
}

type SoftLayerClient interface {
	CancelInstance(id int) error
	CreateInstance(template *datatypes.Virtual_Guest) (datatypes.Virtual_Guest, error)
	GetInstance(id int, mask string) (datatypes.Virtual_Guest, error)
	GetHardware(id int, mask string) (datatypes.Hardware, error)
	GetInstanceByPrimaryBackendIpAddress(ip string) (datatypes.Virtual_Guest, error)
	GetInstanceByPrimaryIpAddress(ip string) (datatypes.Virtual_Guest, error)
	RebootInstance(id int, soft bool, hard bool) error
	ReloadInstance(id int, stemcellId int) error
	UpgradeInstance(id int, cpu int, memory int, network int, privateCPU bool, additional_diskSize int) (datatypes.Container_Product_Order_Receipt, error)
	WaitInstanceUntilReady(id int, until time.Time) error
	WaitInstanceHasActiveTransaction(id int, until time.Time) error
	WaitInstanceHasNoneActiveTransaction(id int, until time.Time) error
	InstanceHasActiveTransaction(id int, until time.Time) (bool, error)
	SetTags(id int, tags string) error
	AttachSecondDiskToInstance(id int, diskSize int) error
	GetInstanceAllowedHost(id int) (datatypes.Network_Storage_Allowed_Host, error)
	AuthorizeHostToVolume(instance *datatypes.Virtual_Guest, volumeId int, until time.Time) (bool, error)
	DeauthorizeHostToVolume(instance *datatypes.Virtual_Guest, volumeId int, until time.Time) (bool, error)
	CreateVolume(storageType string, location string, size int, iops int) (datatypes.Network_Storage, error)
	OrderBlockVolume(storageType string, location string, size int, iops int) (datatypes.Container_Product_Order_Receipt, error)
	CancelBlockVolume(volumeId int, reason string, immedicate bool) error
	GetBlockVolumeDetails(volumeId int, mask string) (datatypes.Network_Storage, error)
	GetImage(imageId int) (datatypes.Virtual_Guest_Block_Device_Template_Group, error)
}

type BmsClient interface {
	OrderHardware(vmNamePrefix string, baremetal_stemcell string, netboot_image string)
	ReloadBaremetal(cid int, stemcell string, netbootImage string)
	CancelHardware(cid int)
	UpdateState(cid string, status string) (bmscli.UpdateStateResponse, error)
	ProvisionBaremetal(server_name string, stemcell string, netboot_image string) (int, error)
	GetHardwareObjectByIpAddress(ip string) (datatypes.Hardware, error)
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

	GetSoftLayerClient()
}

//go:generate counterfeiter -o repfakes/fake_sim_client.go . SimClient
type SimClient interface {
	Client
	Reset() error
}

type client struct {
	slClient         *softlayerClientManager
	bmsClient        *bmscli.BmpClient
	vpsClient        *vpscli.Client
}

func NewClient(slClient *softlayerClientManager, bmsClient *bmscli.BmpClient, vpsClient *vpscli.Client) Client {
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

func (c *client) GetInstance(id int, mask string) (datatypes.Virtual_Guest, error) {
	if mask == "" {
		mask = INSTANCE_DEFAULT_MASK
	}
	return c.slClient.VirtualGuestService.Id(id).Mask(mask).GetObject()
}

func (c *client) GetInstanceByPrimaryBackendIpAddress(ip string) (datatypes.Virtual_Guest, error) {
	filters := filter.New()
	filters = append(filters, filter.Path("virtualGuests.primaryBackendIpAddress").Eq(ip))
	virtualguests, err := c.slClient.AccountService.Mask(INSTANCE_DEFAULT_MASK).Filter(filters.Build()).GetVirtualGuests()
	if err != nil {
		return datatypes.Virtual_Guest{}, nil
	}

	if len(virtualguests) > 0 {
		return virtualguests[0], nil
	}

	return datatypes.Virtual_Guest{}, nil
}

func (c *client) GetInstanceByPrimaryIpAddress(ip string) (datatypes.Virtual_Guest, error) {
	filters := filter.New()
	filters = append(filters, filter.Path("virtualGuests.primaryIpAddress").Eq(ip))
	virtualguests, err := c.slClient.AccountService.Mask(INSTANCE_DEFAULT_MASK).Filter(filters.Build()).GetVirtualGuests()
	if err != nil {
		return datatypes.Virtual_Guest{}, nil
	}

	if len(virtualguests) > 0 {
		return virtualguests[0], nil
	}

	return datatypes.Virtual_Guest{}, nil
}

func (c *client) GetImage(imageId int) (datatypes.Virtual_Guest_Block_Device_Template_Group, error) {
	return c.slClient.ImageService.Id(imageId).Mask(IMAGE_DETAIL_MASK).GetObject()
}

//Check the virtual server instance is ready for use
//param1: bool, indicate whether the instance is ready
//param2: error, any error may happen when getting the status of the instance
func (c *client) WaitInstanceUntilReady(id int, until time.Time) error {
	for {
		virtualGuest, err := c.GetInstance(id, "id, lastOperatingSystemReload[id,modifyDate], activeTransaction[id,transactionStatus.name], provisionDate, powerState.keyName")
		if err != nil {
			return false, bosherr.WrapErrorf(err, "Getting instance with id `%d`", id)
		}

		c.slClient

		lastReload := virtualGuest.LastOperatingSystemReload
		activeTxn := virtualGuest.ActiveTransaction
		provisionDate := virtualGuest.ProvisionDate

		// if lastReload != nil && lastReload.ModifyDate != nil {
		// 	fmt.Println("lastReload: ", (*lastReload.ModifyDate).Format(time.RFC3339))
		// }
		// if activeTxn != nil && activeTxn.TransactionStatus != nil && activeTxn.TransactionStatus.Name != nil {
		// 	fmt.Println("activeTxn: ", *activeTxn.TransactionStatus.Name)
		// }
		// if provisionDate != nil {
		// 	fmt.Println("provisionDate: ", (*provisionDate).Format(time.RFC3339))
		// }

		reloading := activeTxn != nil && lastReload != nil && *activeTxn.Id == *lastReload.Id
		if provisionDate != nil && !reloading {
			//fmt.Println("power state:", *virtualGuest.PowerState.KeyName)
			if *virtualGuest.PowerState.KeyName == "RUNNING" {
				return nil
			}
		}

		now := time.Now()
		if now.After(until) {
			return bosherr.Errorf("Power on virtual guest with id %d Time Out!", *virtualGuest.Id)
		}

		min := math.Min(float64(10.0), float64(until.Sub(now)))
		time.Sleep(time.Duration(min) * time.Second)
	}
}

func (c *client) WaitInstanceHasActiveTransaction(id int, until time.Time) error {
	for {
		virtualGuest, err := c.GetInstance(id, "id, lastOperatingSystemReload[id,modifyDate], activeTransaction[id,transactionStatus.name], provisionDate, powerState.keyName")
		if err != nil {
			return bosherr.WrapErrorf(err, "Getting instance with id `%d`", id)
		}

		// if activeTxn != nil && activeTxn.TransactionStatus != nil && activeTxn.TransactionStatus.Name != nil {
		// 	fmt.Println("activeTxn: ", *activeTxn.TransactionStatus.Name)
		// }

		if virtualGuest.ActiveTransaction != nil {
			return nil
		}

		now := time.Now()
		if now.After(until) {
			return bosherr.Errorf("Wait instance with id of `%d` has active transaction time out", id)
		}

		min := math.Min(float64(5.0), float64(until.Sub(now)))
		time.Sleep(time.Duration(min) * time.Second)
	}
}

func (c *client) WaitInstanceHasNoneActiveTransaction(id int, until time.Time) error {
	for {
		virtualGuest, err := c.GetInstance(id, "id, lastOperatingSystemReload[id,modifyDate], activeTransaction[id,transactionStatus.name], provisionDate, powerState.keyName")
		if err != nil {
			return bosherr.WrapErrorf(err, "Getting instance with id `%d`", id)
		}

		// if activeTxn != nil && activeTxn.TransactionStatus != nil && activeTxn.TransactionStatus.Name != nil {
		// 	fmt.Println("activeTxn: ", *activeTxn.TransactionStatus.Name)
		// }

		if virtualGuest.ActiveTransaction == nil {
			return nil
		}

		now := time.Now()
		if now.After(until) {
			return bosherr.Errorf("Wait instance with id of `%d` has none active transaction time out", id)
		}

		min := math.Min(float64(5.0), float64(until.Sub(now)))
		time.Sleep(time.Duration(min) * time.Second)
	}
}

func (c *client) CreateInstance(template *datatypes.Virtual_Guest) (datatypes.Virtual_Guest, error) {
	virtualguest, err := c.slClient.VirtualGuestService.CreateObject(template)
	if err != nil {
		return datatypes.Virtual_Guest{}, bosherr.WrapError(err, "Creating instance")
	}

	until := time.Now().Add(time.Duration(4) * time.Hour)
	if err := c.WaitInstanceUntilReady(*virtualguest.Id, until); err != nil {
		return datatypes.Virtual_Guest{}, bosherr.WrapError(err, "Waiting until instance is ready")
	}

	return virtualguest, nil
}

func (c *client) ReloadInstance(id int, stemcellId int) error {
	var err error
	until := time.Now().Add(time.Duration(1) * time.Hour)
	if err = c.WaitInstanceHasNoneActiveTransaction(sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance has none active transaction before os_reload")
	}

	config := datatypes.Container_Hardware_Server_Configuration{
		ImageTemplateId: sl.Int(stemcellId),
	}
	_, err = c.slClient.VirtualGuestService.Id(id).ReloadOperatingSystem(sl.String("FORCE"), &config)
	if err != nil {
		return datatypes.Virtual_Guest{}, err
	}

	until = time.Now().Add(time.Duration(1) * time.Hour)
	if err = c.WaitInstanceHasActiveTransaction(sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance has active transaction after launching os_reload")
	}

	until = time.Now().Add(time.Duration(4) * time.Hour)
	if err = c.WaitInstanceUntilReady(sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance is ready after os_reload")
	}

	return nil
}

func (c *client) CancelInstance(id int) error {
	_, err := c.slClient.VirtualGuestService.Id(id).DeleteObject()
	return err
}

func (c *client) GenerateInstanceCreationTemplate(virtualGuest *datatypes.Virtual_Guest, params map[string]interface{}) (*datatypes.Virtual_Guest, error) {
	return &datatypes.Virtual_Guest{}, nil
}

func (c *client) UpgradeInstance(id int, cpu int, memory int, network int, privateCPU bool, additional_diskSize int) (datatypes.Container_Product_Order_Receipt, error) {
	upgradeOptions := make(map[string]int)
	public := true
	if cpu != 0 {
		upgradeOptions["guest_core"] = cpu
	}
	if memory != 0 {
		upgradeOptions["ram"] = memory / 1024
	}
	if network != 0 {
		upgradeOptions["port_speed"] = network
	}
	if privateCPU == true {
		public = false
	}

	packageType := "VIRTUAL_SERVER_INSTANCE"
	productPackages, err := c.slClient.PackageService.
	Mask("id,name,description,isActive,type.keyName").
	Filter(filter.New(filter.Path("type.keyName").Eq(packageType)).Build()).
	GetAllObjects()
	if err != nil {
		return datatypes.Container_Product_Order_Receipt{}, err
	}
	if len(productPackages) == 0 {
		return datatypes.Container_Product_Order_Receipt{}, bosherr.Errorf("No package found for type: %s", packageType)
	}
	packageID := *productPackages[0].Id
	packageItems, err := c.slClient.PackageService.
	Id(packageID).
	Mask("description,capacity,prices[id,locationGroupId,categories]").
	GetItems()
	if err != nil {
		return datatypes.Container_Product_Order_Receipt{}, err
	}
	prices := []datatypes.Product_Item_Price{}
	for option, value := range upgradeOptions {
		priceID := getPriceIdForUpgrade(packageItems, option, value, public)
		if priceID == -1 {
			return datatypes.Container_Product_Order_Receipt{},
			bosherr.Errorf("Unable to find %s option with %d", option,value)
		}
		prices = append(prices, datatypes.Product_Item_Price{Id: &priceID})
	}

	if additional_diskSize != 0 {
		diskItemPrice, err := c.getUpgradeItemPriceForSecondDisk(id, additional_diskSize)
		if err != nil {
			return datatypes.Container_Product_Order_Receipt{}, err
		}
		prices = append(prices, diskItemPrice)
	}

	if len(prices) == 0 {
		return datatypes.Container_Product_Order_Receipt{}, bosherr.Error("Unable to find price for upgrade")
	}
	order := datatypes.Container_Product_Order{
		ComplexType: sl.String(UPGRADE_VIRTUAL_SERVER_ORDER_TYPE),
		Prices:      prices,
		Properties: []datatypes.Container_Product_Order_Property{
			datatypes.Container_Product_Order_Property{
				Name:  sl.String("MAINTENANCE_WINDOW"),
				Value: sl.String(time.Now().UTC().Format(time.RFC3339)),
			},
			datatypes.Container_Product_Order_Property{
				Name:  sl.String("NOTE_GENERAL"),
				Value: sl.String("addingdisks"),
			},
		},
		VirtualGuests: []datatypes.Virtual_Guest{
			datatypes.Virtual_Guest{
				Id: &id,
			},
		},
		PackageId: &packageID,
	}
	upgradeOrder := datatypes.Container_Product_Order_Virtual_Guest_Upgrade{
		Container_Product_Order_Virtual_Guest: datatypes.Container_Product_Order_Virtual_Guest{
			Container_Product_Order_Hardware_Server: datatypes.Container_Product_Order_Hardware_Server{
				Container_Product_Order: order,
			},
		},
	}
	return c.slClient.OrderService.PlaceOrder(&upgradeOrder, sl.Bool(false))
}

func (c *client) SetTags(id int, tags string) error {
	_, err := c.slClient.VirtualGuestService.Id(id).SetTags(&tags)
	return err
}

func (c *client) GetInstanceAllowedHost(id int) (datatypes.Network_Storage_Allowed_Host, error) {
	mask := "id, name, credential[username, password]"
	allowedHost, err := c.slClient.VirtualGuestService.Id(id).Mask(mask).GetAllowedHost()
	if err != nil {
		return datatypes.Network_Storage_Allowed_Host{}, err
	}

	if allowedHost.Id == nil {
		return datatypes.Network_Storage_Allowed_Host{}, bosherr.Errorf("Unable to get allowed host with instance id: %d", id)
	}

	return allowedHost, nil
}

func (c *client) getUpgradeItemPriceForSecondDisk(id int, diskSize int) (datatypes.Product_Item_Price, error) {
	itemPrices, err := c.slClient.VirtualGuestService.Id(id).GetUpgradeItemPrices(sl.Bool(true))
	if err != nil {
		return datatypes.Product_Item_Price{}, err
	}

	var currentDiskCapacity int
	var diskType string
	var currentItemPrice datatypes.Product_Item_Price

	diskTypeBool, err :=c.slClient.VirtualGuestService.Id(id).GetLocalDiskFlag()
	if err != nil {
		return datatypes.Product_Item_Price{}, err
	}

	if diskTypeBool {
		diskType = "(LOCAL)"
	} else {
		diskType = "(SAN)"
	}

	for _, itemPrice := range itemPrices {
		flag := false
		for _, category := range itemPrice.Categories {
			if *category.CategoryCode == EPHEMERAL_DISK_CATEGORY_CODE {
				flag = true
				break
			}
		}

		if flag && strings.Contains(*itemPrice.Item.Description, diskType) {
			if int(*itemPrice.Item.Capacity) >= diskSize {
				if currentItemPrice.Id == nil || currentDiskCapacity >= *itemPrice.Item.Capacity {
					currentItemPrice = itemPrice
					currentDiskCapacity = *itemPrice.Item.Capacity
				}
			}
		}
	}

	if currentItemPrice.Id == nil {
		return datatypes.Product_Item_Price{}, bosherr.Errorf("No proper %s disk for size %d", diskType, diskSize)
	}

	return currentItemPrice, nil
}

func getPriceIdForUpgrade(packageItems []datatypes.Product_Item, option string, value int, public bool) int {
	for _, item := range packageItems {
		isPrivate := strings.HasPrefix(*item.Description, "Private")
		for _, price := range item.Prices {
			if price.LocationGroupId != nil {
				continue
			}
			if len(price.Categories) == 0 {
				continue
			}
			for _, category := range price.Categories {
				if item.Capacity != nil {
					if !(*category.CategoryCode == option && strconv.FormatFloat(float64(*item.Capacity), 'f', 0, 64) == strconv.Itoa(value)) {
						continue
					}
					if option == "guest_core" {
						if public && !isPrivate {
							return *price.Id
						} else if !public && isPrivate {
							return *price.Id
						}
					} else if option == "port_speed" {
						if strings.Contains(*item.Description, "Public") {
							return *price.Id
						}
					} else {
						return *price.Id
					}
				}
			}
		}
	}
	return -1
}

func (c *client) GetBlockVolumeDetails(volumeId int, mask string) (datatypes.Network_Storage, error) {
	if mask == "" {
		mask = VOLUME_DETAIL_MASK
	}
	volume, err := c.slClient.StorageService.Id(volumeId).Mask(mask).GetObject()
	if err != nil {
		return datatypes.Network_Storage{}, err
	}

	return volume, nil
}

func (c *client) OrderBlockVolume(storageType string, location string, size int, iops int) (datatypes.Container_Product_Order_Receipt, error) {
	locationId, err := c.GetLocationId(location)
	if err != nil {
		return datatypes.Container_Product_Order_Receipt{}, bosherr.Error("Invalid datacenter name specified. Please provide the lower case short name (e.g.: dal09)")
	}
	baseTypeName := "SoftLayer_Container_Product_Order_Network_"
	prices := []datatypes.Product_Item_Price{}
	productPacakge, err := c.GetPackage(storageType)
	if err != nil {
		return datatypes.Container_Product_Order_Receipt{}, err
	}

	if storageType == "performance_storage_iscsi" {
		complexType := baseTypeName + "PerformanceStorage_Iscsi"
		storagePrice, err := FindPerformancePrice(productPacakge, "performance_storage_iscsi")
		if err != nil {
			return datatypes.Container_Product_Order_Receipt{}, err
		}
		prices = append(prices, storagePrice)
		spacePrice, err := FindPerformanceSpacePrice(productPacakge, size)
		if err != nil {
			return datatypes.Container_Product_Order_Receipt{}, err
		}
		prices = append(prices, spacePrice)

		var iopsPrice datatypes.Product_Item_Price
		if iops == 0 {
			iopsPrice, err = c.selectMaximunIopsItemPriceIdOnSize(size)
			if err != nil {
				return datatypes.Container_Product_Order_Receipt{}, err
			}
		} else {
			iopsPrice, err = FindPerformanceIOPSPrice(productPacakge, size, iops)
			if err != nil {
				return datatypes.Container_Product_Order_Receipt{}, err
			}
		}
		prices = append(prices, iopsPrice)

		order := datatypes.Container_Product_Order_Network_PerformanceStorage_Iscsi{
			OsFormatType: &datatypes.Network_Storage_Iscsi_OS_Type{
				KeyName: sl.String("Linux"),
			},
			Container_Product_Order_Network_PerformanceStorage: datatypes.Container_Product_Order_Network_PerformanceStorage{
				Container_Product_Order: datatypes.Container_Product_Order{
					ComplexType: sl.String(complexType),
					PackageId:   productPacakge.Id,
					Prices:      prices,
					Quantity:    sl.Int(1),
					Location:    sl.String(strconv.Itoa((locationId))),
				},
			},
		}
		return c.slClient.OrderService.PlaceOrder(&order, sl.Bool(false))
	} else {
		return datatypes.Container_Product_Order_Receipt{}, bosherr.Error("Block volume storage_type must be either Performance or Endurance")
	}
}

func (c *client) CreateVolume(location string, size int, iops int) (datatypes.Network_Storage, error) {
	receipt, err :=c.OrderBlockVolume("performance_storage_iscsi", location, size, iops)
	if err != nil {
		return datatypes.Network_Storage{}, err
	}

	if receipt.OrderId == nil {
		return datatypes.Network_Storage{}, bosherr.Errorf("No order id returned after placing order with size of `%d`, iops of `%d`, location of `%s`",size, iops, location)
	}

	until := time.Now().Add(time.Duration(1) * time.Hour)
	return c.WaitVolumeProvisioningWithOrderId(*receipt.OrderId, until)
}

func (c *client) WaitVolumeProvisioningWithOrderId(orderId int, until time.Time) (datatypes.Network_Storage, error) {
	for {
		volumes, err := c.getIscsiNetworkStorageWithOrderId(orderId)
		if err != nil {
			return datatypes.Network_Storage{}, bosherr.WrapErrorf(err, "Getting volumes with order id  of `%d`", orderId)
		}

		// if activeTxn != nil && activeTxn.TransactionStatus != nil && activeTxn.TransactionStatus.Name != nil {
		// 	fmt.Println("activeTxn: ", *activeTxn.TransactionStatus.Name)
		// }

		if len(volumes) >0 {
			return volumes[0], nil
		}

		now := time.Now()
		if now.After(until) {
			return datatypes.Network_Storage{}, bosherr.Errorf("Waiting volume provisioning with order id of `%d` has time out", orderId)
		}

		min := math.Min(float64(5.0), float64(until.Sub(now)))
		time.Sleep(time.Duration(min) * time.Second)
	}
}

func (c *client) getIscsiNetworkStorageWithOrderId(orderId int) ([]datatypes.Network_Storage, error) {
	filters := filter.New()
	filters = append(filters, filter.Path("iscsiNetworkStorage.billingItem.orderItem.order.id").Eq(orderId))
	return c.slClient.AccountService.Mask(VOLUME_DEFAULT_MASK).Filter(filters.Build()).GetIscsiNetworkStorage()
}

func (c *client) GetPackage(categoryCode string) (datatypes.Product_Package, error) {
	filters := filter.New()
	filters = append(filters, filter.Path("categories.categoryCode").Eq(categoryCode))
	filters = append(filters, filter.Path("statusCode").Eq("ACTIVE"))
	packages, err := c.slClient.PackageService.Mask("id,name,items[prices[categories],attributes]").Filter(filters.Build()).GetAllObjects()
	if err != nil {
		return datatypes.Product_Package{}, err
	}
	if len(packages) == 0 {
		return datatypes.Product_Package{}, bosherr.Errorf("No packages were found for %s ", categoryCode)
	}
	if len(packages) > 1 {
		return datatypes.Product_Package{}, bosherr.Errorf("More than one packages were found for %s", categoryCode)
	}
	return packages[0], nil
}

func (c *client) GetLocationId(location string) (int, error) {
	filter := filter.New(filter.Path("name").Eq(location))
	datacenters, err := c.slClient.LocationService.Mask("longName,id,name").Filter(filter.Build()).GetDatacenters()
	if err != nil {
		return 0, err
	}
	for _, datacenter := range datacenters {
		if *datacenter.Name == location {
			return *datacenter.Id, nil
		}
	}
	return 0, bosherr.Error("Invalid datacenter name specified")
}

func hasCategory(categories []datatypes.Product_Item_Category, categoryCode string) bool {
	for _, category := range categories {
		if *category.CategoryCode == categoryCode {
			return true
		}
	}
	return false
}

//Find the price in the given package that has the specified category
func FindPerformancePrice(productPackage datatypes.Product_Package, priceCategory string) (datatypes.Product_Item_Price, error) {
	for _, item := range productPackage.Items {
		for _, price := range item.Prices {
			// Only collect prices from valid location groups.
			if price.LocationGroupId != nil {
				continue
			}
			if !hasCategory(price.Categories, priceCategory) {
				continue
			}
			return price, nil
		}
	}
	return datatypes.Product_Item_Price{}, bosherr.Error("Could not find price for performance storage")
}

//Find the price in the given package with the specified size
func FindPerformanceSpacePrice(productPackage datatypes.Product_Package, size int) (datatypes.Product_Item_Price, error) {
	for _, item := range productPackage.Items {
		if int(*item.Capacity) != size {
			continue
		}
		for _, price := range item.Prices {
			// Only collect prices from valid location groups.
			if price.LocationGroupId != nil {
				continue
			}
			if !hasCategory(price.Categories, "performance_storage_space") {
				continue
			}
			return price, nil
		}
	}
	return datatypes.Product_Item_Price{}, bosherr.Error("Could not find disk space price for the given volume")
}

//Find the price in the given package with the specified size and iops
func FindPerformanceIOPSPrice(productPackage datatypes.Product_Package, size int, iops int) (datatypes.Product_Item_Price, error) {
	for _, item := range productPackage.Items {
		if int(*item.Capacity) != int(iops) {
			continue
		}
		for _, price := range item.Prices {
			// Only collect prices from valid location groups.
			if price.LocationGroupId != nil {
				continue
			}
			if !hasCategory(price.Categories, "performance_storage_iops") {
				continue
			}
			min, err := strconv.Atoi(*price.CapacityRestrictionMinimum)
			if err != nil {
				return datatypes.Product_Item_Price{}, bosherr.Error("Could not find price for iops for the given volume")
			}
			if size < int(min) {
				continue
			}
			max, err := strconv.Atoi(*price.CapacityRestrictionMaximum)
			if err != nil {
				return datatypes.Product_Item_Price{}, bosherr.Error("Could not find price for iops for the given volume")
			}
			if size > int(max) {
				continue
			}
			return price, nil
		}
	}
	return datatypes.Product_Item_Price{}, bosherr.Error("Could not find price for iops for the given volume")
}

func (c *client) CancelBlockVolume(volumeId int, reason string, immediate bool) error {
	blockVolume, err := c.GetBlockVolumeDetails(volumeId, "id,billingItem.id")
	if err != nil {
		return err
	}
	if blockVolume.BillingItem == nil || blockVolume.BillingItem.Id == nil {
		return bosherr.Error("No billing item is found to cancel")
	}
	billitemId := *blockVolume.BillingItem.Id
	_, err = c.slClient.BillingService.Id(billitemId).CancelItem(sl.Bool(immediate), sl.Bool(true), sl.String(reason), sl.String(""))
	return err
}

func (c *client) AuthorizeHostToVolume(instance *datatypes.Virtual_Guest, volumeId int, until time.Time) (bool, error) {
	for {
		allowable, err := c.slClient.StorageService.Id(volumeId).AllowAccessFromVirtualGuest(instance)
		if err != nil {
			apiErr := err.(sl.Error)
			if apiErr.Exception == "SoftLayer_Exception_ObjectNotFound" {
				return false, bosherr.Errorf(err,"Unable to find object with id of `%d`", volumeId)
			}
			if apiErr.Exception == "SoftLayer_Exception_Network_Storage_BlockingOperationInProgress" {
				continue
			}
			return false, err
		}

		if allowable {
			return true, nil
		}

		now := time.Now()
		if now.After(until) {
			return false, nil
		}

		min := math.Min(float64(5.0), float64(until.Sub(now)))
		time.Sleep(time.Duration(min) * time.Second)
	}
}

func (c *client) DeauthorizeHostToVolume(instance *datatypes.Virtual_Guest, volumeId int, until time.Time) (bool, error){
	for {
		disAllowed, err := c.slClient.StorageService.Id(volumeId).RemoveAccessFromVirtualGuest(instance)
		if err != nil {
			apiErr := err.(sl.Error)
			if apiErr.Exception == "SoftLayer_Exception_ObjectNotFound" {
				return false, bosherr.Errorf("Unable to find object with id of `%d`", volumeId)
			}
			if apiErr.Exception == "SoftLayer_Exception_Network_Storage_BlockingOperationInProgress" {
				continue
			}
			return disAllowed, err
		}

		if disAllowed {
			return true, nil
		}

		now := time.Now()
		if now.After(until) {
			return false, nil
		}

		min := math.Min(float64(5.0), float64(until.Sub(now)))
		time.Sleep(time.Duration(min) * time.Second)
	}
}

func (c *client) AttachSecondDiskToInstance(id int, diskSize int) error {
	_, err := c.UpgradeInstance(id, 0, 0, 0, false, diskSize)
	if err != nil {
		apiErr := err.(sl.Error)
		if strings.Contains(apiErr.Message, "A current price was provided for the upgrade order") {
			return nil
		}
		return bosherr.WrapErrorf(err, "Adding second disk with size `%d` to virutal guest of id `%d`", diskSize, id)
	}
	return nil
}

func (c *client) selectMaximunIopsItemPriceIdOnSize(size int) (datatypes.Product_Item_Price, error) {
	filters := filter.New()
	filters = append(filters, filter.Path("itemPrices.attributes.value").Eq(size))
	filters = append(filters, filter.Path("categories.categoryCode").Eq("performance_storage_iops"))

	itemPrices, err := c.slClient.PackageService.Id(NETWORK_PERFORMANCE_STORAGE_PACKAGE_ID).Filter(filters.Build()).GetItemPrices()
	if err != nil {
		return 0, err
	}

	if len(itemPrices) > 0 {
		candidates := itemsFilter(itemPrices, func(itemPrice datatypes.Product_Item_Price) bool {
			return itemPrice.LocationGroupId == nil
		})
		if len(candidates) > 0 {
			sort.Sort(Product_Item_Price_Sorted_Data(candidates))
			return candidates[len(candidates)-1], nil
		} else {
			return 0, bosherr.Errorf("No proper performance storage (iSCSI volume) for size %d", size)
		}
	}

	return datatypes.Product_Item_Price{}, bosherr.Errorf("No proper performance storage (iSCSI volume)for size %d", size)
}

func (c *client) selectMediumIopsItemPriceIdOnSize(size int) (datatypes.Product_Item_Price, error) {
	filters := filter.New()
	filters = append(filters, filter.Path("itemPrices.attributes.value").Eq(size))
	filters = append(filters, filter.Path("categories.categoryCode").Eq("performance_storage_iops"))

	itemPrices, err := c.slClient.PackageService.Id(NETWORK_PERFORMANCE_STORAGE_PACKAGE_ID).Filter(filters.Build()).GetItemPrices()
	if err != nil {
		return 0, err
	}

	if len(itemPrices) > 0 {
		candidates := itemsFilter(itemPrices, func(itemPrice datatypes.Product_Item_Price) bool {
			return itemPrice.LocationGroupId == nil
		})
		if len(candidates) > 0 {
			sort.Sort(Product_Item_Price_Sorted_Data(candidates))
			return candidates[len(candidates)/2], nil
		} else {
			return 0, bosherr.Errorf("No proper performance storage (iSCSI volume) for size %d", size)
		}
	}

	return datatypes.Product_Item_Price{}, bosherr.Errorf("No proper performance storage (iSCSI volume)for size %d", size)
}

func itemsFilter(vs []datatypes.Product_Item_Price, f func(datatypes.Product_Item_Price) bool) []datatypes.Product_Item_Price {
	vsf := make([]datatypes.Product_Item_Price, 0)
	for _, v := range vs {
		if f(v) {
			vsf = append(vsf, v)
		}
	}

	return vsf
}

type Product_Item_Price_Sorted_Data []datatypes.Product_Item_Price

func (sorted_data Product_Item_Price_Sorted_Data) Len() int {
	return len(sorted_data)
}

func (sorted_data Product_Item_Price_Sorted_Data) Swap(i, j int) {
	sorted_data[i], sorted_data[j] = sorted_data[j], sorted_data[i]
}

func (sorted_data Product_Item_Price_Sorted_Data) Less(i, j int) bool {
	value1, err := strconv.Atoi(sorted_data[i].Item.Capacity)
	if err != nil {
		return false
	}
	value2, err := strconv.Atoi(sorted_data[j].Item.Capacity)
	if err != nil {
		return false
	}

	return value1 < value2
}