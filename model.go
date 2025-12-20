package minizinc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Model struct {
	mu sync.RWMutex

	codeFragments []string
	dataFiles     []string
	parameters    map[string]interface{}
	assigned      map[string]bool
}

func NewModel() *Model {
	return &Model{
		codeFragments: make([]string, 0),
		dataFiles:     make([]string, 0),
		parameters:    make(map[string]interface{}),
		assigned:      make(map[string]bool),
	}
}

func (m *Model) AddString(code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codeFragments = append(m.codeFragments, code)
}

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

func (m *Model) SetParam(name string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.assigned[name] {
		return ErrMultipleAssignment
	}

	m.parameters[name] = value
	m.assigned[name] = true

	return nil
}

func (m *Model) GetParam(name string) (interface{}, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	val, ok := m.parameters[name]
	return val, ok
}

func (m *Model) Copy() *Model {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cloned := &Model{
		codeFragments: make([]string, len(m.codeFragments)),
		dataFiles:     make([]string, len(m.dataFiles)),
		parameters:    make(map[string]interface{}),
		assigned:      make(map[string]bool),
	}

	for i, frag := range m.codeFragments {
		cloned.codeFragments[i] = frag
	}

	for i, file := range m.dataFiles {
		cloned.dataFiles[i] = file
	}

	for k, v := range m.parameters {
		cloned.parameters[k] = deepCopyValue(v)
	}

	for k, v := range m.assigned {
		cloned.assigned[k] = v
	}

	return cloned
}

func deepCopyValue(v interface{}) interface{} {
	switch val := v.(type) {
	case []interface{}:
		cp := make([]interface{}, len(val))
		for i, item := range val {
			cp[i] = deepCopyValue(item)
		}
		return cp
	case map[string]interface{}:
		cp := make(map[string]interface{})
		for k, item := range val {
			cp[k] = deepCopyValue(item)
		}
		return cp
	default:
		return v
	}
}

func (m *Model) getCode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := ""
	for _, fragment := range m.codeFragments {
		result += fragment + "\n"
	}
	return result
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
