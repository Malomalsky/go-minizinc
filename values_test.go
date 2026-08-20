package minizinc

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSetJSON(t *testing.T) {
	value := Set[int]{
		Elements: []int{1, 8},
		Ranges:   []SetRange[int]{{Min: 3, Max: 5}},
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"set":[1,8,[3,5]]}` {
		t.Fatalf("got %s", data)
	}

	var decoded Set[int]
	if err := json.Unmarshal([]byte(`{"set":[1,[3,5],8]}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Elements, []int{1, 8}) ||
		!reflect.DeepEqual(decoded.Ranges, []SetRange[int]{{Min: 3, Max: 5}}) {
		t.Fatalf("got %+v", decoded)
	}

	data, err = json.Marshal(Set[int]{})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"set":[]}` {
		t.Fatalf("empty set got %s", data)
	}
}

func TestSetJSONRejectsMalformedValues(t *testing.T) {
	values := []string{
		`null`,
		`{}`,
		`{"set":null}`,
		`{"set":{}}`,
		`{"set":[[1]]}`,
		`{"set":[[1,2,3]]}`,
		`{"set":["bad"]}`,
		`{"set":[],"extra":true}`,
	}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			var set Set[int]
			if err := json.Unmarshal([]byte(value), &set); err == nil {
				t.Fatalf("accepted %s", value)
			}
		})
	}
}

func TestEnumJSON(t *testing.T) {
	tests := []struct {
		name  string
		value Enum
		json  string
	}{
		{name: "value", value: Enum{Value: "Canada"}, json: `{"e":"Canada"}`},
		{name: "anonymous", value: AnonymousEnum("Slot", 2), json: `{"e":"Slot","i":2}`},
		{name: "nested", value: Enum{Constructor: "C", Argument: Enum{Value: "Canada"}}, json: `{"c":"C","e":{"e":"Canada"}}`},
		{name: "integer", value: Enum{Constructor: "Right", Argument: 2}, json: `{"c":"Right","e":2}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != test.json {
				t.Fatalf("got %s", data)
			}
			var decoded Enum
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			data, err = json.Marshal(decoded)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != test.json {
				t.Fatalf("round trip got %s", data)
			}
		})
	}
}

func TestEnumJSONDecodesLegacyConstructor(t *testing.T) {
	var value Enum
	if err := json.Unmarshal([]byte(`{"c":"C","e":"Canada"}`), &value); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"c":"C","e":{"e":"Canada"}}` {
		t.Fatalf("got %s", data)
	}
}

func TestEnumJSONRejectsMalformedValues(t *testing.T) {
	values := []string{
		`null`,
		`{}`,
		`{"e":""}`,
		`{"e":2}`,
		`{"e":"Slot","i":null}`,
		`{"e":"Slot","i":1.5}`,
		`{"e":"Slot","i":"2"}`,
		`{"e":"Slot","i":2,"extra":true}`,
		`{"c":"C"}`,
		`{"c":"","e":2}`,
		`{"c":"C","e":null}`,
		`{"c":"C","e":[]}`,
		`{"c":"C","e":{}}`,
		`{"e":"Canada","extra":true}`,
	}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			var enum Enum
			if err := json.Unmarshal([]byte(value), &enum); err == nil {
				t.Fatalf("accepted %s", value)
			}
		})
	}
}

func TestEnumJSONRejectsInvalidGoValues(t *testing.T) {
	var argument *Enum
	values := []Enum{
		{},
		{Value: "Canada", Argument: 1},
		{Value: "Canada", Constructor: "C", Argument: 1},
		{Constructor: "C"},
		{Constructor: "C", Argument: argument},
		{Value: "Slot", Constructor: "C", Index: new(int)},
		{Value: "Slot", Argument: 1, Index: new(int)},
	}
	for _, value := range values {
		if _, err := json.Marshal(value); err == nil {
			t.Fatalf("accepted %+v", value)
		}
	}
}

func TestMiniZincValuesThroughParamsAndDecode(t *testing.T) {
	set := Set[int]{Elements: []int{5}, Ranges: []SetRange[int]{{Min: 1, Max: 3}}}
	enum := Enum{Constructor: "Right", Argument: 2}
	model := NewModel("solve satisfy;")
	if err := model.SetParam("allowed", set); err != nil {
		t.Fatal(err)
	}
	if err := model.SetParam("node", enum); err != nil {
		t.Fatal(err)
	}
	data, err := model.getDataJSON(nil)
	if err != nil {
		t.Fatal(err)
	}
	if data != `{"allowed":{"set":[5,[1,3]]},"node":{"c":"Right","e":2}}` {
		t.Fatalf("got %s", data)
	}

	result := &Result{Solution: map[string]any{
		"allowed": map[string]any{"set": []any{json.Number("5"), []any{json.Number("1"), json.Number("3")}}},
		"node":    map[string]any{"c": "Right", "e": json.Number("2")},
	}}
	var decoded struct {
		Allowed Set[int] `json:"allowed"`
		Node    Enum     `json:"node"`
	}
	if err := result.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Allowed, set) {
		t.Fatalf("set got %+v", decoded.Allowed)
	}
	if decoded.Node.Constructor != "Right" || decoded.Node.Argument != json.Number("2") {
		t.Fatalf("enum got %+v", decoded.Node)
	}
}
