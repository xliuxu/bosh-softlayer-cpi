package disk

import (
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	slc "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"
)

const SOFTLAYER_DISK_LOG_TAG = "SoftLayerDisk"

type SoftLayerDisk struct {
	id     int
	client slc.Client
	logger boshlog.Logger
}

func NewSoftLayerDisk(id int, client slc.Client, logger boshlog.Logger) SoftLayerDisk {
	return SoftLayerDisk{
		id:              id,
		client: client,
		logger:          logger,
	}
}

func (s SoftLayerDisk) ID() int { return s.id }

func (s SoftLayerDisk) Delete() error {
	s.logger.Debug(SOFTLAYER_DISK_LOG_TAG, "Deleting disk '%s'", s.id)

	err := s.client.CancelBlockVolume(s.id, "", true)
	if err != nil {
		return bosherr.WrapError(err, "Deleting disk from SoftLayer")
	}

	return nil
}
