package cypress

import (
	"errors"
	"reflect"
)

var (
	// ErrPointerRequired a pointer is required
	ErrPointerRequired = errors.New("a pointer is required")
)

type smartMapper struct {
	valueType reflect.Type
}

// NewSmartMapper creates a smart row mapper for data row
func NewSmartMapper(value interface{}) RowMapper {
	return &smartMapper{reflect.TypeOf(value)}
}

// Map maps the data row to a value object
func (mapper *smartMapper) Map(row DataRow) (interface{}, error) {
	columns, err := row.Columns()
	if err != nil {
		return nil, err
	}

	columnTypes, err := row.ColumnTypes()
	if err != nil {
		return nil, err
	}

	if len(columnTypes) == 1 {
		t := mapper.valueType
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}

		if t.Kind() != reflect.Struct {
			value := reflect.New(t)
			row.Scan(value.Interface())
			return value.Elem().Interface(), nil
		}
	}

	valueType := mapper.valueType
	if valueType.Kind() != reflect.Ptr {
		return nil, ErrPointerRequired
	}

	valueType = valueType.Elem()
	getters := GetFieldValueGetters(valueType)
	value := reflect.New(valueType)
	values := make([]interface{}, len(columns))

	for index, name := range columns {
		getter, ok := getters[name]
		if ok {
			values[index] = getter.Get(value.Elem()).Addr().Interface()
		} else {
			values[index] = &values[index]
		}
	}

	row.Scan(values...)
	return value.Interface(), nil
}
