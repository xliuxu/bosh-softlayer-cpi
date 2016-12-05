package stemcell

import (
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	slc "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer"
)

type SoftLayerStemcellFinder struct {
	client slc.Client
	logger boshlog.Logger
}

func NewSoftLayerStemcellFinder(client slc.Client, logger boshlog.Logger) SoftLayerStemcellFinder {
	return SoftLayerStemcellFinder{client: client, logger: logger}
}

func (f SoftLayerStemcellFinder) FindById(id int) (Stemcell, error) {
	vgbdtg, err := f.client.GetImage(id)
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Getting Image with id of `%d`", id)
	}
	return NewSoftLayerStemcell(vgbdtg.Id, vgbdtg.GlobalIdentifier), nil
}
