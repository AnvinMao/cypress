package cypress

import (
	"testing"
)

func TestConcurrentMap(t *testing.T) {
	m := NewConcurrentMap()
	m.Put("key1", "value1")
	m.Put("key2", 100)

	v1, ok := m.Get("key1")
	if !ok {
		t.Error("key1 must be there")
		return
	}

	if v1.(string) != "value1" {
		t.Error("value for key1 expected to be value1 but got", v1.(string))
	}

	v2, ok := m.Get("key2")
	if !ok {
		t.Error("key2 must be there")
		return
	}

	if v2.(int) != 100 {
		t.Error("value for key2 expected to be 100 but got", v2.(int))
	}

	v3 := m.GetOrCompute("key3", func() interface{} { return 400 })
	if v3 != 400 {
		t.Error("unexpected value for v3", v3, "expected 400")
		return
	}

	v4, ok := m.Get("key3")
	if !ok {
		t.Error("key3 must be there")
		return
	}

	if v4.(int) != 400 {
		t.Error("value for key3 expected to be 400 but got", v4.(int))
	}
}
