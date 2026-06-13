package minizinc

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

// Model is a constraint problem under construction: MiniZinc code fragments,
// data files and named parameters. Methods are safe for concurrent use.
type Model struct {
	mu sync.RWMutex

	codeFragments  []string
	dataFiles      []string
	parameters     map[string]any
	assigned       map[string]bool
	requiredParams []string // populated by Builder.Build; checked at solve time
}

// NewModel returns an empty Model.
func NewModel() *Model {
	return &Model{
		codeFragments: make([]string, 0),
		dataFiles:     make([]string, 0),
		parameters:    make(map[string]any),
		assigned:      make(map[string]bool),
	}
}

// AddString appends a MiniZinc code fragment to the model.
func (m *Model) AddString(code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codeFragments = append(m.codeFragments, code)
}

// AddFile loads a .mzn model file (inlined into code fragments) or a .dzn /
// .json data file (added to the data-file list passed to MiniZinc via -d).
func (m *Model) AddFile(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := os.Stat(path); err != nil {
		return wrapError("file not found", err)
	}

	ext := filepath.Ext(path)
	switch ext {
	case ".mzn":
		content, err := os.ReadFile(path)
		if err != nil {
			return wrapError("failed to read model file", err)
		}
		m.codeFragments = append(m.codeFragments, string(content))
	case ".dzn", ".json":
		m.dataFiles = append(m.dataFiles, path)
	default:
		return newError(fmt.Sprintf("unsupported file extension: %s", ext))
	}

	return nil
}

// SetParam records a named parameter value, serialized to JSON when the model
// is solved. Each name may only be assigned once; subsequent assignments
// return ErrMultipleAssignment.
func (m *Model) SetParam(name string, value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.assigned[name] {
		return ErrMultipleAssignment
	}

	m.parameters[name] = value
	m.assigned[name] = true

	return nil
}

// GetParam returns the value of a parameter and whether it was set.
func (m *Model) GetParam(name string) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	val, ok := m.parameters[name]
	return val, ok
}

// Copy returns a deep copy of the model. Parameter values are deep-copied via
// reflection so slice/map mutations on the copy do not affect the original.
func (m *Model) Copy() *Model {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cloned := &Model{
		codeFragments:  make([]string, len(m.codeFragments)),
		dataFiles:      make([]string, len(m.dataFiles)),
		parameters:     make(map[string]any, len(m.parameters)),
		assigned:       make(map[string]bool, len(m.assigned)),
		requiredParams: append([]string(nil), m.requiredParams...),
	}

	copy(cloned.codeFragments, m.codeFragments)
	copy(cloned.dataFiles, m.dataFiles)

	for k, v := range m.parameters {
		cloned.parameters[k] = deepCopyValue(v)
	}
	maps.Copy(cloned.assigned, m.assigned)

	return cloned
}

// MissingParams returns names of required parameters that have not been
// assigned via SetParam. Empty result means the model is ready to solve as
// far as Builder-recorded requirements go.
func (m *Model) MissingParams() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var missing []string
	for _, name := range m.requiredParams {
		if !m.assigned[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func deepCopyValue(v any) any {
	if v == nil {
		return nil
	}
	return deepCopyReflect(reflect.ValueOf(v)).Interface()
}

func deepCopyReflect(v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		dst := reflect.MakeSlice(v.Type(), v.Len(), v.Cap())
		for i := 0; i < v.Len(); i++ {
			dst.Index(i).Set(deepCopyReflect(v.Index(i)))
		}
		return dst
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		dst := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			dst.SetMapIndex(deepCopyReflect(iter.Key()), deepCopyReflect(iter.Value()))
		}
		return dst
	case reflect.Array:
		dst := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			dst.Index(i).Set(deepCopyReflect(v.Index(i)))
		}
		return dst
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		dst := reflect.New(v.Elem().Type())
		dst.Elem().Set(deepCopyReflect(v.Elem()))
		return dst
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		inner := deepCopyReflect(v.Elem())
		out := reflect.New(v.Type()).Elem()
		out.Set(inner)
		return out
	default:
		return v
	}
}

func (m *Model) getCode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder
	for _, fragment := range m.codeFragments {
		sb.WriteString(fragment)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (m *Model) getDataJSON() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.parameters) == 0 {
		return "", nil
	}

	data, err := json.Marshal(m.parameters)
	if err != nil {
		return "", wrapError("failed to marshal parameters", err)
	}

	return string(data), nil
}
