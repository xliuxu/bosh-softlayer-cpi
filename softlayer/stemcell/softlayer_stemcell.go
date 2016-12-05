package stemcell

type SoftLayerStemcell struct {
	id   int
	uuid string
}

func NewSoftLayerStemcell(id int, uuid string) SoftLayerStemcell {
	return SoftLayerStemcell{
		id:              id,
		uuid:            uuid,
	}
}

func (s SoftLayerStemcell) ID() int { return s.id }

func (s SoftLayerStemcell) Uuid() string { return s.uuid }

func (s SoftLayerStemcell) Delete() error {
	return nil
}
