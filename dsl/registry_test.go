package dsl

import (
	"reflect"
	"testing"
)

var mappingType = reflect.TypeOf((*Mapping[any, any])(nil))

func TestFlagOpts_ExistOnMapping(t *testing.T) {
	for _, name := range FlagOpts {
		if _, ok := mappingType.MethodByName(name); !ok {
			t.Errorf("FlagOpts %q: no method Mapping.%s() — add it to dsl.go", name, name)
		}
	}
}

func TestMethodDefs_ExistOnMapping(t *testing.T) {
	for name := range MethodDefs {
		if _, ok := mappingType.MethodByName(name); !ok {
			t.Errorf("MethodDefs %q: no method Mapping.%s() — add it to dsl.go", name, name)
		}
	}
}
