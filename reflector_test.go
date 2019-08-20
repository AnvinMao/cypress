package cypress

import (
	"reflect"
	"testing"
)

type innerStruct struct {
	Value  float64 `col:"fvalue"`
	Value2 string  `alias:"svalue"`
	Root   *testStruct
}

type dumpStruct struct {
	Value    int          `alias:"value"`
	ValuePtr *int         `col:"ptr"`
	Inner    *innerStruct `prefix:"i_"`
}

type testStruct struct {
	Field1 string
	Field2 dumpStruct `prefix:"dump_"`
}

func TestFieldGetters(t *testing.T) {
	obj := &testStruct{}
	getters := GetFieldValueGetters(reflect.TypeOf(obj).Elem())
	val := reflect.ValueOf(obj)
	getters["Field1"].Get(val.Elem()).SetString("field1")
	getters["dump_value"].Get(val.Elem()).SetInt(100)
	getters["dump_ptr"].Get(val.Elem()).SetInt(200)
	getters["dump_i_fvalue"].Get(val.Elem()).SetFloat(300.0)
	getters["dump_i_svalue"].Get(val.Elem()).SetString("svalue")
	if obj.Field1 != "field1" {
		t.Error("Field1 not set, expect field1 but get value", obj.Field1)
		return
	}

	if obj.Field2.Value != 100 {
		t.Error("Field2.Value not set, expect 100 but get value", obj.Field2.Value)
		return
	}

	if *obj.Field2.ValuePtr != 200 {
		t.Error("Field2.ValuePtr not set, expect 200 but get value", *obj.Field2.ValuePtr)
		return
	}

	if obj.Field2.Inner.Value != 300.0 {
		t.Error("Field2.Inner.Value not set, expect 300.0 but get value", obj.Field2.Inner.Value)
		return
	}

	if obj.Field2.Inner.Value2 != "svalue" {
		t.Error("Field2.Inner.Value2 not set, expected svalue but get value", obj.Field2.Inner.Value2)
		return
	}
}
