package schema

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/The-Orbits/orbits-go/internal/yaml"
)

// Example builds a value that matches schema, with every optional property present, so a test can check that a
// Go type has a field for each. variant picks among alternatives (oneOf, enum values) and, when odd, makes required
// nullable properties null. The result is made of map[string]any, []any, string, float64, bool and nil.
func (s *Spec) Example(schema any, variant int) any {
	return s.example(schema, variant, false, 0)
}

func (s *Spec) example(schema any, variant int, allowNull bool, depth int) any {
	if depth > 24 {
		return nil
	}
	alts := s.alternatives(schema)
	var usable [][]*yaml.Map
	var nulls [][]*yaml.Map
	for _, a := range alts {
		if onlyNull(a) {
			nulls = append(nulls, a)
		} else {
			usable = append(usable, a)
		}
	}
	if allowNull && variant%2 == 1 && len(nulls) > 0 {
		return nil
	}
	if len(usable) == 0 {
		return nil
	}
	set := usable[variant%len(usable)]

	for _, m := range set {
		if Has(m, "const") {
			return jsonValue(Get(m, "const"))
		}
	}
	for _, m := range set {
		if enum := List(m, "enum"); enum != nil {
			var values []any
			for _, e := range enum {
				if e != nil {
					values = append(values, e)
				}
			}
			if allowNull && variant%2 == 1 && len(values) < len(enum) {
				return nil
			}
			if len(values) > 0 {
				return jsonValue(values[variant%len(values)])
			}
		}
	}
	types := setTypes(set)
	if allowNull && variant%2 == 1 && contains(types, "null") {
		return nil
	}
	typ := ""
	for _, t := range types {
		if t != "null" {
			typ = t
			break
		}
	}
	if typ == "" {
		if o := s.objectView(set); o.constrained {
			typ = "object"
		} else if hasItems(set) {
			typ = "array"
		}
	}
	switch typ {
	case "object":
		return s.exampleObject(set, variant, depth)
	case "array":
		n := 1
		var items any
		for _, m := range set {
			if v, ok := num(m, "minItems"); ok && int(v) > n {
				n = int(v)
			}
			if it := Get(m, "items"); it != nil {
				items = it
			}
		}
		out := make([]any, n)
		for i := range out {
			out[i] = s.example(items, variant+i, false, depth+1)
		}
		return out
	case "string":
		return exampleString(set)
	case "integer":
		return exampleNumber(set, true)
	case "number":
		return exampleNumber(set, false)
	case "boolean":
		return true
	}
	// No type: any JSON.
	return map[string]any{"example": true}
}

func onlyNull(set []*yaml.Map) bool {
	if len(set) == 0 {
		return false
	}
	sawNull := false
	for _, m := range set {
		types := Types(m)
		if len(types) == 1 && types[0] == "null" {
			sawNull = true
		}
	}
	return sawNull
}

func setTypes(set []*yaml.Map) []string {
	for _, m := range set {
		if t := Types(m); len(t) > 0 {
			return t
		}
	}
	return nil
}

func hasItems(set []*yaml.Map) bool {
	for _, m := range set {
		if Has(m, "items") {
			return true
		}
	}
	return false
}

func (s *Spec) exampleObject(set []*yaml.Map, variant, depth int) any {
	o := s.objectView(set)
	out := map[string]any{}
	for _, name := range o.names {
		out[name] = s.example(o.props[name][0], variant, o.required[name], depth+1)
	}
	// A required property the schemas do not declare (an abstract base such as Page) gets the extra schema's
	// example, or null.
	for _, name := range sortedKeys(o.required) {
		if _, ok := out[name]; !ok {
			var v any
			if len(o.extra) > 0 {
				v = s.example(o.extra[0], variant, false, depth+1)
			}
			out[name] = v
		}
	}
	if len(o.names) == 0 && o.open && len(o.required) == 0 {
		if len(o.extra) > 0 {
			out["example_key"] = s.example(o.extra[0], variant, false, depth+1)
		} else {
			out["example_key"] = "example"
		}
	}
	return out
}

// jsonValue turns a YAML scalar into what encoding/json would decode.
func jsonValue(v any) any {
	if n, ok := v.(int64); ok {
		return float64(n)
	}
	return v
}

var literalPrefix = regexp.MustCompile(`^\^([A-Za-z0-9_]+)`)

func exampleString(set []*yaml.Map) string {
	var candidates []string
	for _, m := range set {
		for _, e := range List(m, "examples") {
			if s, ok := e.(string); ok {
				candidates = append(candidates, s)
			}
		}
	}
	format := ""
	for _, m := range set {
		if f := Str(m, "format"); f != "" {
			format = f
		}
		if p := literalPrefix.FindStringSubmatch(Str(m, "pattern")); p != nil {
			candidates = append(candidates, p[1]+"0001", p[1]+"example", p[1]+"0123456789abcdef")
		}
	}
	switch format {
	case "date-time":
		candidates = append(candidates, "2026-11-10T09:30:00Z")
	case "date":
		candidates = append(candidates, "2026-11-10")
	case "uri":
		candidates = append(candidates, "https://example.com/path")
	case "uri-reference":
		candidates = append(candidates, "/v1/example")
	case "email":
		candidates = append(candidates, "person@example.com")
	}
	candidates = append(candidates, "example", "example-1", "EXAMPLE_KEY", "#1a2b3c", "2026-11", "0123456789abcdef", "abc", "a", "ab")
	for _, c := range candidates {
		ok := true
		for _, m := range set {
			if len(checkString(m, c)) > 0 {
				ok = false
				break
			}
		}
		if ok {
			return c
		}
	}
	return candidates[0]
}

func exampleNumber(set []*yaml.Map, integer bool) any {
	lo, hi := math.Inf(-1), math.Inf(1)
	exclusive := false
	for _, m := range set {
		if v, ok := num(m, "minimum"); ok {
			lo = math.Max(lo, v)
		}
		if v, ok := num(m, "exclusiveMinimum"); ok {
			lo = math.Max(lo, v)
			exclusive = true
		}
		if v, ok := num(m, "maximum"); ok {
			hi = math.Min(hi, v)
		}
	}
	v := 1.0
	if !integer {
		v = 1.5
	}
	switch {
	case v < lo || (exclusive && v <= lo):
		v = lo + 1
	case v > hi:
		v = hi
	}
	if !math.IsInf(lo, 0) && !math.IsInf(hi, 0) && (v < lo || v > hi) {
		v = (lo + hi) / 2
	}
	if integer {
		v = math.Ceil(v)
	}
	return v
}

// Describe is a short text for a schema in messages: its $ref name, or its type.
func Describe(schema any) string {
	if name := RefName(schema); name != "" {
		return name
	}
	if m, ok := schema.(*yaml.Map); ok {
		if t := Types(m); len(t) > 0 {
			return strings.Join(t, "|")
		}
	}
	return fmt.Sprintf("%T", schema)
}
