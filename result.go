package minizinc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Result is one solver outcome — solution variables, terminal status, and
// (when available) statistics. Error is populated only by SolveStream when
// the streaming pipeline produces an error.
//
// IsIntermediate is set only by SolveStream. It is true for every solution
// emitted before the terminal status message arrives (improving solutions
// during optimization) and false for the last result, which carries the
// terminal Status. Consumers that only care about the final answer can
// filter on !IsIntermediate.
//
// Sections collects any string-valued entries reported by MiniZinc's
// `output [...]` / `output_to_section()` in the solution message, keyed by
// section name. The default "dzn" section is consumed to populate Solution
// and is NOT duplicated here.
type Result struct {
	Status         Status
	Solution       map[string]any
	Statistics     Statistics
	Error          error
	IsIntermediate bool
	Sections       map[string]string
}

// Section returns a named output section if reported.
func (r *Result) Section(name string) (string, bool) {
	v, ok := r.Sections[name]
	return v, ok
}

func (r *Result) Get(name string) (any, bool) {
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

// Decode populates dst from the solution map using JSON struct tags.
// dst must be a pointer to a struct (or compatible type for json.Unmarshal).
// Field tags follow standard `json:"name"` conventions; field names match
// solution keys case-insensitively as json.Unmarshal does.
func (r *Result) Decode(dst any) error {
	if r == nil {
		return newError("nil result")
	}
	data, err := json.Marshal(r.Solution)
	if err != nil {
		return wrapError("failed to marshal solution", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return wrapError("failed to decode solution", err)
	}
	return nil
}

func (r *Result) GetArray(name string) ([]any, error) {
	val, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("variable %s not found", name)
	}

	arr, ok := val.([]any)
	if !ok {
		return nil, fmt.Errorf("variable %s is not an array", name)
	}

	return arr, nil
}

func parseDZN(dzn string) map[string]any {
	result := make(map[string]any)

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

		var value any
		if err := json.Unmarshal([]byte(valueStr), &value); err != nil {
			result[name] = valueStr
		} else {
			result[name] = value
		}
	}

	return result
}

func parseStreamMessage(msg streamMessage) (*Result, error) {
	status := msg.Status
	if status == "" && msg.Type == "solution" {
		status = StatusSatisfied
	}
	result := &Result{
		Status:   status,
		Solution: make(map[string]any),
	}

	if msg.Type == "solution" && msg.Solution != nil {
		result.Solution = msg.Solution
	}

	if msg.Type == "solution" && msg.Output != nil {
		if dzn, ok := msg.Output["dzn"].(string); ok && len(result.Solution) == 0 {
			result.Solution = parseDZN(dzn)
		}
		for k, v := range msg.Output {
			if k == "dzn" {
				continue
			}
			if s, ok := v.(string); ok {
				if result.Sections == nil {
					result.Sections = make(map[string]string)
				}
				result.Sections[k] = s
			}
		}
	}

	return result, nil
}

func parseStatisticsFromMessage(msg streamMessage) (Statistics, bool) {
	stats := msg.Statistics
	if stats == nil && msg.Output != nil {
		if raw, ok := msg.Output["statistics"]; ok {
			if m, ok := raw.(map[string]any); ok {
				stats = m
			}
		}
	}
	if stats == nil {
		return Statistics{}, false
	}
	return parseStatistics(stats), true
}

func parseStatistics(stats map[string]any) Statistics {
	var out Statistics

	setInt := func(dst *int64, key string) {
		if v, ok := stats[key]; ok {
			if n, ok := numberToInt64(v); ok {
				*dst = n
			}
		}
	}

	setDuration := func(dst *time.Duration, key string) {
		if v, ok := stats[key]; ok {
			if f, ok := numberToFloat64(v); ok {
				*dst = time.Duration(f * float64(time.Second))
			}
		}
	}

	setInt(&out.Nodes, "nodes")
	setInt(&out.Failures, "failures")
	setInt(&out.RestartCount, "restarts")
	setInt(&out.Variables, "variables")
	setInt(&out.PropagatorRuns, "propagations")
	setInt(&out.Propagations, "propags")
	setInt(&out.PeakDepth, "peakDepth")
	setInt(&out.NoGoods, "nogoods")
	setInt(&out.Backtracks, "backjumps")
	setInt(&out.Paths, "nPaths")

	setDuration(&out.SolveTime, "solveTime")
	setDuration(&out.InitTime, "initTime")
	setDuration(&out.FlatTime, "flatTime")

	return out
}

func numberToFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func numberToInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return i, true
		}
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return int64(f), true
	default:
		return 0, false
	}
}
