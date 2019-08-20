package cypress

import (
	"reflect"
	"sync"
)

// ConcurrentMap a concurrent map
type ConcurrentMap struct {
	typeEnforced bool
	enforcedType reflect.Type
	lock         *sync.RWMutex
	values       map[string]interface{}
}

// NewConcurrentMap creates a new instance of ConcurrentMap
func NewConcurrentMap() *ConcurrentMap {
	return &ConcurrentMap{false, reflect.TypeOf(false), &sync.RWMutex{}, make(map[string]interface{})}
}

// NewConcurrentMapTypeEnforced create a new instance of ConcurrentMap with
// enforcement of the value type
func NewConcurrentMapTypeEnforced(valueType reflect.Type) *ConcurrentMap {
	return &ConcurrentMap{true, valueType, &sync.RWMutex{}, make(map[string]interface{})}
}

// Put puts a value to the map associate to the map and return the old value
func (m *ConcurrentMap) Put(key string, value interface{}) (interface{}, bool) {
	if m.typeEnforced && !reflect.TypeOf(value).AssignableTo(m.enforcedType) {
		panic("Type for map is enforced to " + m.enforcedType.String())
	}

	m.lock.Lock()
	defer m.lock.Unlock()
	oldValue, ok := m.values[key]
	m.values[key] = value
	return oldValue, ok
}

// Foreach iterates the map and passes the key and value to the given function
func (m *ConcurrentMap) Foreach(f func(key string, value interface{})) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	for k, v := range m.values {
		f(k, v)
	}
}

// RemoveIf iterates the map and delete all items that the evaluator returns true
// returns number of items that were removed
func (m *ConcurrentMap) RemoveIf(evaluator func(key string, value interface{}) bool) int {
	m.lock.Lock()
	defer m.lock.Unlock()
	keysToRemove := make([]string, 0)
	for k, v := range m.values {
		if evaluator(k, v) {
			keysToRemove = append(keysToRemove, k)
		}
	}

	for _, k := range keysToRemove {
		delete(m.values, k)
	}

	return len(keysToRemove)
}

// Delete deletes the specified key from the map
func (m *ConcurrentMap) Delete(key string) {
	m.lock.Lock()
	defer m.lock.Unlock()
	delete(m.values, key)
}

// Get gets a value for the given key if it exists
func (m *ConcurrentMap) Get(key string) (interface{}, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	value, ok := m.values[key]
	return value, ok
}

// GetOrCompute gets a value from map if it does not exist
// compute the value from the given generator
func (m *ConcurrentMap) GetOrCompute(key string, generator func() interface{}) interface{} {
	var value interface{}
	var ok bool
	func() {
		m.lock.RLock()
		defer m.lock.RUnlock()
		value, ok = m.values[key]
	}()

	if ok {
		return value
	}

	m.lock.Lock()
	defer m.lock.Unlock()
	value, ok = m.values[key]
	if !ok {
		value = generator()
		if m.typeEnforced && !reflect.TypeOf(value).AssignableTo(m.enforcedType) {
			panic("Type for map is enforced to " + m.enforcedType.String())
		}

		m.values[key] = value
	}

	return value
}
