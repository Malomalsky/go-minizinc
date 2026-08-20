package minizinc

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// Model is a constraint problem under construction: MiniZinc code fragments,
// data files and named parameters. Methods are safe for concurrent use.
type Model struct {
	mu sync.RWMutex

	codeFragments  []string
	dataFiles      []string
	includeDirs    []string
	parameters     map[string]any
	assigned       map[string]bool
	requiredParams []string // populated by Builder.Build; checked at solve time
}

func NewModel(code ...string) *Model {
	return &Model{
		codeFragments: append([]string(nil), code...),
		dataFiles:     make([]string, 0),
		parameters:    make(map[string]any),
		assigned:      make(map[string]bool),
	}
}

func LoadModel(path string) (*Model, error) {
	model := NewModel()
	if err := model.AddFile(path); err != nil {
		return nil, err
	}
	return model, nil
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

	absPath, err := filepath.Abs(path)
	if err != nil {
		return wrapError("failed to resolve file path", err)
	}
	if _, err := os.Stat(absPath); err != nil {
		return wrapError("file not found", err)
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	switch ext {
	case ".mzn":
		content, err := os.ReadFile(absPath)
		if err != nil {
			return wrapError("failed to read model file", err)
		}
		m.codeFragments = append(m.codeFragments, string(content))
		dir := filepath.Dir(absPath)
		if !slices.Contains(m.includeDirs, dir) {
			m.includeDirs = append(m.includeDirs, dir)
		}
	case ".dzn", ".json":
		m.dataFiles = append(m.dataFiles, absPath)
	default:
		return newError(fmt.Sprintf("unsupported file extension: %s", ext))
	}

	return nil
}

// SetParam records a named parameter value, serialized to JSON when the model
// is solved. Each name may only be assigned once; subsequent assignments
// return ErrMultipleAssignment.
func (m *Model) SetParam(name string, value any) error {
	if _, err := json.Marshal(value); err != nil {
		return wrapError("invalid parameter value", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.assigned[name] {
		return ErrMultipleAssignment
	}
	m.parameters[name] = deepCopyValue(value)
	m.assigned[name] = true

	return nil
}

// GetParam returns the value of a parameter and whether it was set.
func (m *Model) GetParam(name string) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	val, ok := m.parameters[name]
	return deepCopyValue(val), ok
}

// Copy returns a deep copy of the model. Parameter values are deep-copied via
// reflection so slice/map mutations on the copy do not affect the original.
func (m *Model) Copy() *Model {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cloned := &Model{
		codeFragments:  make([]string, len(m.codeFragments)),
		dataFiles:      make([]string, len(m.dataFiles)),
		includeDirs:    slices.Clone(m.includeDirs),
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
	return m.missingParams(nil)
}

func (m *Model) missingParams(params Params) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var missing []string
	for _, name := range m.requiredParams {
		if !m.assigned[name] {
			if _, ok := params[name]; !ok {
				missing = append(missing, name)
			}
		}
	}
	return missing
}

func cloneParams(params Params) (Params, error) {
	if len(params) == 0 {
		return nil, nil
	}
	if _, err := json.Marshal(params); err != nil {
		return nil, wrapError("invalid parameter value", err)
	}
	return deepCopyValue(params).(Params), nil
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
	case reflect.Struct:
		dst := reflect.New(v.Type()).Elem()
		dst.Set(v)
		for i := 0; i < v.NumField(); i++ {
			if dst.Field(i).CanSet() {
				dst.Field(i).Set(deepCopyReflect(v.Field(i)))
			}
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

func (m *Model) getDataJSON(params Params) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	merged := maps.Clone(m.parameters)
	if merged == nil {
		merged = make(map[string]any)
	}
	maps.Copy(merged, params)
	if len(merged) == 0 {
		return "", nil
	}

	data, err := json.Marshal(merged)
	if err != nil {
		return "", wrapError("failed to marshal parameters", err)
	}

	return string(data), nil
}
