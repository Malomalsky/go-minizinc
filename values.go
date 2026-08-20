package minizinc

import (
	"bytes"
	"encoding/json"
	"io"
)

type SetRange[T any] struct {
	Min T
	Max T
}

type Set[T any] struct {
	Elements []T
	Ranges   []SetRange[T]
}

func (s Set[T]) MarshalJSON() ([]byte, error) {
	items := make([]any, 0, len(s.Elements)+len(s.Ranges))
	for _, element := range s.Elements {
		items = append(items, element)
	}
	for _, itemRange := range s.Ranges {
		items = append(items, [2]T{itemRange.Min, itemRange.Max})
	}
	return json.Marshal(struct {
		Set []any `json:"set"`
	}{Set: items})
}

func (s *Set[T]) UnmarshalJSON(data []byte) error {
	if s == nil {
		return newError("cannot unmarshal set into nil receiver")
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return wrapError("invalid MiniZinc set", err)
	}
	raw, ok := object["set"]
	if !ok || len(object) != 1 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return newError("invalid MiniZinc set")
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return wrapError("invalid MiniZinc set", err)
	}
	elements := make([]T, 0, len(items))
	ranges := make([]SetRange[T], 0)
	for _, item := range items {
		trimmed := bytes.TrimSpace(item)
		if len(trimmed) > 0 && trimmed[0] == '[' {
			var bounds []json.RawMessage
			if err := json.Unmarshal(item, &bounds); err != nil {
				return wrapError("invalid MiniZinc set range", err)
			}
			if len(bounds) != 2 {
				return newError("invalid MiniZinc set range")
			}
			var itemRange SetRange[T]
			if err := json.Unmarshal(bounds[0], &itemRange.Min); err != nil {
				return wrapError("invalid MiniZinc set range minimum", err)
			}
			if err := json.Unmarshal(bounds[1], &itemRange.Max); err != nil {
				return wrapError("invalid MiniZinc set range maximum", err)
			}
			ranges = append(ranges, itemRange)
			continue
		}

		var element T
		if err := json.Unmarshal(item, &element); err != nil {
			return wrapError("invalid MiniZinc set element", err)
		}
		elements = append(elements, element)
	}

	s.Elements = elements
	s.Ranges = ranges
	return nil
}

type Enum struct {
	Value       string
	Constructor string
	Argument    any
}

func (e Enum) MarshalJSON() ([]byte, error) {
	if e.Value != "" && e.Constructor == "" && e.Argument == nil {
		return json.Marshal(struct {
			Value string `json:"e"`
		}{Value: e.Value})
	}
	if e.Value == "" && e.Constructor != "" && e.Argument != nil {
		argument, err := json.Marshal(e.Argument)
		if err != nil {
			return nil, wrapError("invalid MiniZinc enum constructor argument", err)
		}
		if _, err := decodeEnumArgument(argument); err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Constructor string          `json:"c"`
			Argument    json.RawMessage `json:"e"`
		}{Constructor: e.Constructor, Argument: argument})
	}
	return nil, newError("invalid MiniZinc enum")
}

func (e *Enum) UnmarshalJSON(data []byte) error {
	if e == nil {
		return newError("cannot unmarshal enum into nil receiver")
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return wrapError("invalid MiniZinc enum", err)
	}
	argument, hasArgument := object["e"]
	constructor, hasConstructor := object["c"]
	if !hasArgument {
		return newError("invalid MiniZinc enum")
	}
	if !hasConstructor {
		if len(object) != 1 {
			return newError("invalid MiniZinc enum")
		}
		var value string
		if err := json.Unmarshal(argument, &value); err != nil || value == "" {
			return newError("invalid MiniZinc enum value")
		}
		*e = Enum{Value: value}
		return nil
	}
	if len(object) != 2 {
		return newError("invalid MiniZinc enum")
	}

	var name string
	if err := json.Unmarshal(constructor, &name); err != nil || name == "" {
		return newError("invalid MiniZinc enum constructor")
	}
	value, err := decodeEnumArgument(argument)
	if err != nil {
		return err
	}
	*e = Enum{Constructor: name, Argument: value}
	return nil
}

func decodeEnumArgument(data []byte) (any, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var nested Enum
		if err := json.Unmarshal(trimmed, &nested); err != nil {
			return nil, err
		}
		return nested, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, wrapError("invalid MiniZinc enum constructor argument", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, newError("invalid MiniZinc enum constructor argument")
	}
	number, ok := value.(json.Number)
	if !ok {
		return nil, newError("invalid MiniZinc enum constructor argument")
	}
	if _, err := number.Int64(); err != nil {
		return nil, wrapError("invalid MiniZinc enum constructor argument", err)
	}
	return number, nil
}
