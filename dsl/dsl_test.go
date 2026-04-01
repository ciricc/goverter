package dsl_test

import (
	"strconv"

	. "github.com/jmattheis/goverter/dsl"
)

type InputUser struct {
	FirstName string
	LastName  string
	Age       int
	Address   InputAddress
	Internal  string
}

type InputAddress struct {
	City   string
	Street string
}

type OutputUser struct {
	Name       string
	AgeStr     string
	City       string
	Street     string
	InternalID int
}

type OutputAddress struct {
	City   string
	Street string
}

type UserConverter interface {
	ConvertUser(src InputUser) OutputUser
	ConvertAddress(src InputAddress) OutputAddress
}

var _ = Conv[UserConverter](
	Method(UserConverter.ConvertUser, func(m *Mapping[InputUser, OutputUser]) {
		m.Map(m.From.FirstName, m.To.Name)
		m.MapCustom(m.From.Age, m.To.AgeStr, strconv.Itoa)
		m.AutoMap(m.From.Address)
		m.Ignore(m.To.InternalID)
	}),

	Method(UserConverter.ConvertAddress, func(m *Mapping[InputAddress, OutputAddress]) {}),
)
