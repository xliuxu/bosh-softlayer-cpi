package vm

import (
	"fmt"
	"strings"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"

	sl "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"
)

type softLayerVMDeleter struct {
	softLayerClient sl.Client
	logger          boshlog.Logger
}

func NewSoftLayerVMDeleter(softLayerClient sl.Client, logger boshlog.Logger) VMDeleter {
	return &softLayerVMDeleter{
		softLayerClient: softLayerClient,
		logger:          logger,
	}
}

func (c *softLayerVMDeleter) Delete(cid int) error {
	virtualGuestService, err := c.softLayerClient.GetSoftLayer_Virtual_Guest_Service()
	if err != nil {
		return bosherr.WrapError(err, "Creating SoftLayer VirtualGuestService from client")
	}

	_, err = virtualGuestService.DeleteObject(cid)
	if err != nil {
		if !strings.Contains(err.Error(), "HTTP error code") {
			return bosherr.WrapError(err, "Deleting SoftLayer VirtualGuest from client")
		}
	}

	return nil
}
