package action

import (
	"time"

	"github.com/cloudfoundry/bosh-softlayer-cpi/api"

	bslcstem "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/stemcell"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
)

type CreateStemcellAction struct {
	stemcellFinder bslcstem.StemcellFinder
}

type CreateStemcellCloudProps struct {
	Id             int    `json:"virtual-disk-image-id"`
	Uuid           string `json:"virtual-disk-image-uuid"`
	DatacenterName string `json:"datacenter-name"`
}

func NewCreateStemcell(
	stemcellFinder bslcstem.StemcellFinder,
) (action CreateStemcellAction) {
	action.stemcellFinder = stemcellFinder
	return
}

func (a CreateStemcellAction) Run(imagePath string, stemcellCloudProps CreateStemcellCloudProps) (string, error) {
	api.TIMEOUT = 30 * time.Second
	api.POLLING_INTERVAL = 5 * time.Second

	stemcell, err := a.stemcellFinder.FindById(stemcellCloudProps.Id)
	if err != nil {
		return "0", bosherr.WrapErrorf(err, "Finding stemcell with ID '%d'", stemcellCloudProps.Id)
	}

	return StemcellCID(stemcell.ID()).String(), nil
}
