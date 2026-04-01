package dsl_test

import . "github.com/jmattheis/goverter/dsl"

type Item struct {
	Title string
	Price int
}

type ItemDTO struct {
	Label string
	Cost  int
}

type ItemConverter interface {
	ToDTO(src Item) ItemDTO
}

var _ = Conv[ItemConverter](
	Method(ItemConverter.ToDTO, func(m *Mapping[Item, ItemDTO]) {
		m.Map(m.From.Title, m.To.Label)
		m.Map(m.From.Price, m.To.Cost)
	}),
)
