package disk

import (
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	slc "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"
)

type SoftLayerCreator struct {
	softLayerClient slc.Client
	logger          boshlog.Logger
}

func NewSoftLayerDiskCreator(client slc.Client, logger boshlog.Logger) SoftLayerCreator {
	return SoftLayerCreator{
		softLayerClient: client,
		logger:          logger,
	}
}

func (c SoftLayerCreator) Create(size int, datacenter string, cloudProps DiskCloudProperties,) (Disk, error) {
	size = c.getSoftLayerDiskSize(size)
	disk, err := c.softLayerClient.CreateVolume(datacenter, size, cloudProps.Iops)
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Ordering Performance iSCSI disk with disk size `%dG`, iops `%d`", size, cloudProps.Iops)
	}

	return NewSoftLayerDisk(disk.Id, c.softLayerClient, c.logger), nil
}

func (c SoftLayerCreator) getSoftLayerDiskSize(size int) int {
	// Sizes and IOPS ranges: http://knowledgelayer.softlayer.com/learning/performance-storage-concepts
	sizeArray := []int{20, 40, 80, 100, 250, 500, 1000, 2000, 4000, 8000, 12000}

	for _, value := range sizeArray {
		if ret := size / 1024; ret <= value {
			return value
		}
	}
	return 12000
}
