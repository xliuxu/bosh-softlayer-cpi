package vm

import (
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"

	slc "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"
)

type softLayerVMDeleter struct {
	client slc.Client
	logger boshlog.Logger
}

func NewSoftLayerVMDeleter(softLayerClient slc.Client, logger boshlog.Logger) VMDeleter {
	return &softLayerVMDeleter{
		client: softLayerClient,
		logger:          logger,
	}
}

func (deleter *softLayerVMDeleter) Delete(cid int) error {
	return deleter.client.CancelInstance(cid)
}
