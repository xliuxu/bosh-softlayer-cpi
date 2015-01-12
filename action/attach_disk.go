package action

import (
	bosherr "github.com/cloudfoundry/bosh-agent/errors"

	bslcdisk "github.com/maximilien/bosh-softlayer-cpi/softlayer/disk"
	bslcvm "github.com/maximilien/bosh-softlayer-cpi/softlayer/vm"
)

type AttachDisk struct {
	vmFinder   bslcvm.Finder
	diskFinder bslcdisk.Finder
}

func NewAttachDisk(vmFinder bslcvm.Finder, diskFinder bslcdisk.Finder) AttachDisk {
	return AttachDisk{
		vmFinder:   vmFinder,
		diskFinder: diskFinder,
	}
}

func (a AttachDisk) Run(vmCID VMCID, diskCID DiskCID) (interface{}, error) {
	vm, found, err := a.vmFinder.Find(int(vmCID))
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Finding VM '%s'", vmCID)
	}

	if !found {
		return nil, bosherr.Errorf("Expected to find VM '%s'", vmCID)
	}

	disk, found, err := a.diskFinder.Find(int(diskCID))
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Finding disk '%s'", diskCID)
	}

	if !found {
		return nil, bosherr.Errorf("Expected to find disk '%s'", diskCID)
	}

	err = vm.AttachDisk(disk)
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Attaching disk '%s' to VM '%s'", diskCID, vmCID)
	}

	return nil, nil
}
