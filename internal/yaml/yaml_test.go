package yaml

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// plain turns a parsed document into plain maps, for comparing.
func plain(v any) any {
	switch x := v.(type) {
	case *Map:
		out := map[string]any{}
		for _, k := range x.Keys {
			out[k] = plain(x.Values[k])
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plain(e)
		}
		return out
	case int64:
		return float64(x)
	}
	return v
}

func TestUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want any
	}{
		{"scalars", "a: 1\nb: 1.5\nc: true\nd: null\ne: text here\nf: '202'\ng: \"x\\ny\"\nh: ~\n",
			map[string]any{"a": 1.0, "b": 1.5, "c": true, "d": nil, "e": "text here", "f": "202", "g": "x\ny", "h": nil}},
		{"nested", "a:\n  b:\n    c: d\n  e: f\n", map[string]any{"a": map[string]any{"b": map[string]any{"c": "d"}, "e": "f"}}},
		{"sequence", "a:\n  - x\n  - y\n", map[string]any{"a": []any{"x", "y"}}},
		{"sequence at key indent", "a:\n- x\n- y\nb: z\n", map[string]any{"a": []any{"x", "y"}, "b": "z"}},
		{"compact mapping in sequence", "a:\n  - name: n\n    in: query\n  - name: m\n", map[string]any{"a": []any{
			map[string]any{"name": "n", "in": "query"}, map[string]any{"name": "m"}}}},
		{"flow", "a: { type: string, enum: [x, 'y z', null], n: 3 }\n", map[string]any{"a": map[string]any{
			"type": "string", "enum": []any{"x", "y z", nil}, "n": 3.0}}},
		{"flow over lines", "a: { type: string,\n     enum: [x,\n       y] }\nb: c\n", map[string]any{"a": map[string]any{
			"type": "string", "enum": []any{"x", "y"}}, "b": "c"}},
		{"flow key without value", "a: { description: one, two }\n", map[string]any{"a": map[string]any{"description": "one", "two": nil}}},
		{"literal", "a: |\n  one\n\n  two\nb: c\n", map[string]any{"a": "one\n\ntwo\n", "b": "c"}},
		{"literal strip", "a: |-\n  one\n  two\n", map[string]any{"a": "one\ntwo"}},
		{"folded", "a: >\n  one\n  two\n\n  three\n", map[string]any{"a": "one two\nthree\n"}},
		{"comments", "# top\na: b # trailing\n# between\nc: 'd # not a comment'\n", map[string]any{"a": "b", "c": "d # not a comment"}},
		{"plain over lines", "a: one\n  two\nb: c\n", map[string]any{"a": "one two", "b": "c"}},
		{"quoted keys", "'200': ok\n\"a b\": c\n", map[string]any{"200": "ok", "a b": "c"}},
		{"url values", "a: https://example.com/x\nb: { url: 'https://example.com' }\n", map[string]any{"a": "https://example.com/x", "b": map[string]any{"url": "https://example.com"}}},
		{"single quote escape", "a: 'it''s'\n", map[string]any{"a": "it's"}},
		{"empty value", "a:\nb: c\n", map[string]any{"a": nil, "b": "c"}},
		{"dash then block", "a:\n  -\n    b: c\n", map[string]any{"a": []any{map[string]any{"b": "c"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Unmarshal([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			if g := plain(got); !reflect.DeepEqual(g, tt.want) {
				t.Errorf("got  %#v\nwant %#v", g, tt.want)
			}
		})
	}
}

func TestUnmarshalErrors(t *testing.T) {
	for _, src := range []string{"a: [x, y\n", "a: 'open\n", "a: b\na: c\n", "a: { b: c } d\n", "\ta: b\n"} {
		if _, err := Unmarshal([]byte(src)); err == nil {
			t.Errorf("%q: want an error", src)
		}
	}
}

func TestKeepsKeyOrder(t *testing.T) {
	v, err := Unmarshal([]byte("z: 1\na: 2\nm: { y: 1, b: 2 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(*Map)
	if !reflect.DeepEqual(m.Keys, []string{"z", "a", "m"}) {
		t.Errorf("keys %v", m.Keys)
	}
	if inner := m.Values["m"].(*Map); !reflect.DeepEqual(inner.Keys, []string{"y", "b"}) {
		t.Errorf("flow keys %v", inner.Keys)
	}
}

// TestSpec parses the vendored API spec. With YAML_DUMP set it also writes the result there as JSON, to compare
// with another parser's reading.
func TestSpec(t *testing.T) {
	src, err := os.ReadFile("../../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	v, err := Unmarshal(src)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(*Map)
	if !ok {
		t.Fatalf("the spec is a %T", v)
	}
	paths, _ := m.Get("paths")
	if pm, ok := paths.(*Map); !ok || len(pm.Keys) < 100 {
		t.Fatalf("expected the spec's paths, got %T", paths)
	}
	if out := os.Getenv("YAML_DUMP"); out != "" {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", " ")
		if err := enc.Encode(plain(v)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
