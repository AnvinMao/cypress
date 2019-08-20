package cypress

import (
	"reflect"
	"sync"

	"github.com/golang-collections/collections/stack"
)

var (
	globalGettersCache = &gettersCache{make(map[string]map[string]*FieldValueGetter), &sync.RWMutex{}}
)

// FieldValueGetter the field value pointer retriever
type FieldValueGetter struct {
	name   string
	parent *FieldValueGetter
}

type gettersCache struct {
	cache map[string]map[string]*FieldValueGetter
	lock  *sync.RWMutex
}

// NewFieldValueGetter creates a new FieldValueGetter object
func NewFieldValueGetter(fieldName string) *FieldValueGetter {
	return &FieldValueGetter{fieldName, nil}
}

// Get gets the field value object, the field value object should be settable
// which means if the value's type is an pointer, then it should be pointing
// to a valid memory
func (getter *FieldValueGetter) Get(value reflect.Value) reflect.Value {
	if !value.CanAddr() {
		panic("value must be addressable")
	}

	thisValue := value
	if getter.parent != nil {
		thisValue = getter.parent.Get(thisValue)
	}

	fieldValue := thisValue.FieldByName(getter.name)
	if fieldValue.Type().Kind() == reflect.Ptr {
		if fieldValue.IsNil() {
			fieldObject := reflect.New(fieldValue.Type().Elem())
			fieldValue.Set(fieldObject)
		}

		return fieldValue.Elem()
	}

	return fieldValue
}

// GetFieldValueGetters gets all possible FieldValueGetters for the
// give type t
func GetFieldValueGetters(t reflect.Type) map[string]*FieldValueGetter {
	typeName := t.PkgPath() + "/" + t.Name()
	var cache map[string]*FieldValueGetter
	var ok bool
	func() {
		globalGettersCache.lock.RLock()
		defer globalGettersCache.lock.RUnlock()
		cache, ok = globalGettersCache.cache[typeName]
	}()
	if ok {
		return cache
	}

	// we allow concurrent runs to create more than one set of getters for
	// the same type, which just some compute resources
	getters := make(map[string]*FieldValueGetter)
	type stackItem struct {
		Types  []reflect.Type
		Getter *FieldValueGetter
		Prefix string
	}

	buildStack := stack.New()
	buildStack.Push(&stackItem{[]reflect.Type{t}, nil, ""})
	for buildStack.Len() > 0 {
		item := buildStack.Pop()
		current := item.(*stackItem)
		currentType := current.Types[len(current.Types)-1]
		for i := 0; i < currentType.NumField(); i++ {
			field := currentType.Field(i)
			tag := field.Tag
			name := tag.Get("alias")
			if name == "" {
				name = tag.Get("col")
			}

			if name == "" {
				name = field.Name
			}

			if field.Type.Kind() == reflect.Struct || (field.Type.Kind() == reflect.Ptr && field.Type.Elem().Kind() == reflect.Struct) {
				fieldType := field.Type
				if fieldType.Kind() == reflect.Ptr {
					fieldType = fieldType.Elem()
				}

				// check for circular references
				circularlyRef := false
				for _, prevType := range current.Types {
					if fieldType.AssignableTo(prevType) {
						circularlyRef = true
						break
					}
				}

				// breaks the circularly reference
				if circularlyRef {
					continue
				}

				prefix := tag.Get("prefix")
				if prefix == "" {
					prefix = field.Name + "_"
				}

				getter := NewFieldValueGetter(field.Name)
				if current.Getter != nil {
					getter.parent = current.Getter
				}

				typeChain := make([]reflect.Type, len(current.Types)+1)
				copy(typeChain, current.Types)
				typeChain[len(typeChain)-1] = fieldType
				buildStack.Push(&stackItem{typeChain, getter, current.Prefix + prefix})
			} else {
				g := NewFieldValueGetter(field.Name)
				if current.Getter != nil {
					g.parent = current.Getter
				}

				getters[current.Prefix+name] = g
			}
		}
	}

	globalGettersCache.lock.Lock()
	defer globalGettersCache.lock.Unlock()
	_, ok = globalGettersCache.cache[typeName]
	if !ok {
		globalGettersCache.cache[typeName] = getters
	}

	return getters
}
