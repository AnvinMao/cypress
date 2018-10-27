package cypress

import (
	"errors"
	"reflect"
	"sync"
)

var (
	// ErrPointerRequired a pointer is required
	ErrPointerRequired = errors.New("a pointer is required")

	// ErrUnknownColumn no field to map the column
	ErrUnknownColumn = errors.New("don't know how to map the column")
)
var fieldNameCache = newNameMappingCache()

type cacheEntry struct {
	cache map[string]string
	lock  *sync.RWMutex
}

type nameMappingCache struct {
	cache map[string]*cacheEntry
	lock  *sync.RWMutex
}

type smartMapper struct {
	value interface{}
}

func newNameMappingCache() *nameMappingCache {
	return &nameMappingCache{make(map[string]*cacheEntry), &sync.RWMutex{}}
}

func (c *nameMappingCache) getCacheEntry(typeName string) *cacheEntry {
	c.lock.RLock()
	entry, ok := c.cache[typeName]
	c.lock.RUnlock()
	if !ok {
		c.lock.Lock()
		entry, ok = c.cache[typeName]
		if !ok {
			entry = &cacheEntry{make(map[string]string), &sync.RWMutex{}}
			c.cache[typeName] = entry
		}

		c.lock.Unlock()
	}

	return entry
}

func (c *nameMappingCache) get(typeName, columnName string) (string, bool) {
	entry := c.getCacheEntry(typeName)
	entry.lock.RLock()
	defer entry.lock.RUnlock()
	value, ok := entry.cache[columnName]
	return value, ok
}

func (c *nameMappingCache) put(typeName, columnName, fieldName string) {
	entry := c.getCacheEntry(typeName)
	entry.lock.Lock()
	defer entry.lock.Unlock()
	entry.cache[columnName] = fieldName
}

// NewSmartMapper creates a smart row mapper for data row
func NewSmartMapper(value interface{}) RowMapper {
	return &smartMapper{value}
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
		t := reflect.TypeOf(mapper.value)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}

		if t.Kind() != reflect.Struct {
			value := reflect.New(t)
			row.Scan(value.Interface())
			return value.Elem().Interface(), nil
		}
	}

	valueType := reflect.TypeOf(mapper.value)
	if valueType.Kind() != reflect.Ptr {
		return nil, ErrPointerRequired
	}

	valueType = valueType.Elem()
	typeID := valueType.PkgPath() + "/" + valueType.Name()
	value := reflect.New(valueType)
	values := make([]interface{}, len(columns))

	for index, name := range columns {
		fieldName, ok := fieldNameCache.get(typeID, name)
		if !ok {
			_, ok := valueType.FieldByName(name)
			if !ok {
				for i := 0; i < valueType.NumField(); i = i + 1 {
					f := valueType.Field(i)
					if name == f.Tag.Get("col") {
						fieldName = f.Name
						break
					}
				}
			} else {
				fieldName = name
			}

			if fieldName != "" {
				fieldNameCache.put(typeID, name, fieldName)
			}
		}

		if fieldName == "" {
			return nil, ErrUnknownColumn
		}

		fieldValue := value.Elem().FieldByName(fieldName)
		if fieldValue.Type().Kind() == reflect.Ptr {
			values[index] = fieldValue.Interface()
		} else {
			values[index] = fieldValue.Addr().Interface()
		}
	}

	row.Scan(values...)
	return value.Interface(), nil
}
