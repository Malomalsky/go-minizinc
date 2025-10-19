package minizinc

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Result struct {
	Status     Status
	Solution   map[string]interface{}
	Statistics Statistics
	Error      error
}

func (r *Result) Get(name string) (interface{}, bool) {
	val, ok := r.Solution[name]
	return val, ok
}

func (r *Result) GetInt(name string) (int, error) {
	val, ok := r.Get(name)
	if !ok {
		return 0, fmt.Errorf("variable %s not found", name)
	}

	switch v := val.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	case json.Number:
		i, err := v.Int64()
		return int(i), err
	default:
		return 0, fmt.Errorf("variable %s is not an integer", name)
	}
}

func (r *Result) GetFloat(name string) (float64, error) {
	val, ok := r.Get(name)
	if !ok {
		return 0, fmt.Errorf("variable %s not found", name)
	}

	switch v := val.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case json.Number:
		return v.Float64()
	default:
		return 0, fmt.Errorf("variable %s is not a float", name)
	}
}

func (r *Result) GetBool(name string) (bool, error) {
	val, ok := r.Get(name)
	if !ok {
		return false, fmt.Errorf("variable %s not found", name)
	}

	b, ok := val.(bool)
	if !ok {
		return false, fmt.Errorf("variable %s is not a boolean", name)
	}

	return b, nil
}

func (r *Result) GetString(name string) (string, error) {
	val, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("variable %s not found", name)
	}

	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("variable %s is not a string", name)
	}

	return s, nil
}

func (r *Result) GetArray(name string) ([]interface{}, error) {
	val, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("variable %s not found", name)
	}

	arr, ok := val.([]interface{})
	if !ok {
		return nil, fmt.Errorf("variable %s is not an array", name)
	}

	return arr, nil
}

func parseDZN(dzn string) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	lines := strings.Split(dzn, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "=") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		name := strings.TrimSpace(parts[0])
		valueStr := strings.TrimSpace(parts[1])
		valueStr = strings.TrimSuffix(valueStr, ";")
		valueStr = strings.TrimSpace(valueStr)

		var value interface{}
		if err := json.Unmarshal([]byte(valueStr), &value); err != nil {
			result[name] = valueStr
		} else {
			result[name] = value
		}
	}

	return result, nil
}

func parseStreamMessage(msg streamMessage) (*Result, error) {
	result := &Result{
		Status:   msg.Status,
		Solution: make(map[string]interface{}),
	}

	if msg.Type == "solution" && msg.Output != nil {
		if dzn, ok := msg.Output["dzn"].(string); ok {
			parsed, err := parseDZN(dzn)
			if err != nil {
				return nil, err
			}
			result.Solution = parsed
		}
	}

	return result, nil
}
