package dsl_test

import . "github.com/jmattheis/goverter/dsl"

type Employee struct {
	FirstName string
	LastName  string
	DeptID    int
}

type EmployeeDTO struct {
	FullName   string
	Department string
}

type EmployeeConverter interface {
	Convert(src Employee) (EmployeeDTO, error)
	ConvertList(src []Employee) ([]EmployeeDTO, error)
}

func GetFullName(e Employee) string       { return e.FirstName + " " + e.LastName }
func DeptIDToName(id int) (string, error) { return "Engineering", nil }

var _ = Conv[EmployeeConverter](
	WrapErrors(),
	Extend(DeptIDToName),

	Method(EmployeeConverter.Convert, func(m *Mapping[Employee, EmployeeDTO]) {
		m.MapCustom(Source, m.To.FullName, GetFullName)
		m.Map(m.From.DeptID, m.To.Department)
	}),

	Method(EmployeeConverter.ConvertList, func(m *Mapping[Employee, EmployeeDTO]) {}),
)
