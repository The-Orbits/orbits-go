// Package schema reads the vendored OpenAPI document: its operations, its schemas with $ref resolved, a strict
// validator for JSON values against a schema, and a generator of example values from a schema. The SDK's type
// generator and its spec tests are built on it.
package schema

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/The-Orbits/orbits-go/internal/yaml"
)

// Spec is a parsed OpenAPI document.
type Spec struct {
	Doc *yaml.Map
}

// Load reads and parses the document at path.
func Load(path string) (*Spec, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(src)
}

// Parse parses a document.
func Parse(src []byte) (*Spec, error) {
	v, err := yaml.Unmarshal(src)
	if err != nil {
		return nil, err
	}
	m, ok := v.(*yaml.Map)
	if !ok {
		return nil, fmt.Errorf("schema: the document is a %T, not a mapping", v)
	}
	return &Spec{Doc: m}, nil
}

// Operation is one method on one path.
type Operation struct {
	ID          string
	Method      string // upper case
	Path        string // as in the spec, e.g. /teams/{team}/projects
	Tags        []string
	Description string
	// Params are the path-level and operation-level parameters, $ref resolved, operation-level last.
	Params []*yaml.Map
	// Body is the application/json request body's schema, or nil.
	Body         any
	BodyRequired bool
	Responses    []Response
}

// Response is one status of an operation, with the schema of its first content type.
type Response struct {
	Status      string
	ContentType string
	Schema      any
	Description string
}

// Param returns the parameter called name in in ("query", "path", "header"), or nil.
func (o *Operation) Param(in, name string) *yaml.Map {
	for _, p := range o.Params {
		if Str(p, "in") == in && Str(p, "name") == name {
			return p
		}
	}
	return nil
}

// Success returns the first 2xx response that has a JSON body, or nil.
func (o *Operation) Success() *Response {
	for i := range o.Responses {
		r := &o.Responses[i]
		if strings.HasPrefix(r.Status, "2") && r.ContentType == "application/json" {
			return r
		}
	}
	return nil
}

var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// Operations lists every operation, in document order.
func (s *Spec) Operations() []Operation {
	var out []Operation
	paths := Map(s.Doc, "paths")
	if paths == nil {
		return nil
	}
	for _, path := range paths.Keys {
		item := Map(paths, path)
		var shared []*yaml.Map
		for _, p := range List(item, "parameters") {
			if pm, ok := s.Deref(p).(*yaml.Map); ok {
				shared = append(shared, pm)
			}
		}
		for _, m := range item.Keys {
			if !contains(methods, m) {
				continue
			}
			om := Map(item, m)
			op := Operation{ID: Str(om, "operationId"), Method: strings.ToUpper(m), Path: path, Description: Str(om, "description")}
			for _, t := range List(om, "tags") {
				op.Tags = append(op.Tags, fmt.Sprint(t))
			}
			op.Params = append(op.Params, shared...)
			for _, p := range List(om, "parameters") {
				if pm, ok := s.Deref(p).(*yaml.Map); ok {
					// An operation-level parameter replaces a path-level one of the same name.
					op.Params = replaceParam(op.Params, pm)
				}
			}
			if rb, ok := s.Deref(get(om, "requestBody")).(*yaml.Map); ok {
				op.BodyRequired = Bool(rb, "required")
				if js := Map(Map(rb, "content"), "application/json"); js != nil {
					op.Body = get(js, "schema")
				}
			}
			if rs := Map(om, "responses"); rs != nil {
				for _, code := range rs.Keys {
					r, _ := s.Deref(get(rs, code)).(*yaml.Map)
					resp := Response{Status: code, Description: Str(r, "description")}
					if c := Map(r, "content"); c != nil && len(c.Keys) > 0 {
						resp.ContentType = c.Keys[0]
						resp.Schema = get(Map(c, c.Keys[0]), "schema")
					}
					op.Responses = append(op.Responses, resp)
				}
			}
			out = append(out, op)
		}
	}
	return out
}

func replaceParam(params []*yaml.Map, p *yaml.Map) []*yaml.Map {
	for i, q := range params {
		if Str(q, "name") == Str(p, "name") && Str(q, "in") == Str(p, "in") {
			out := append([]*yaml.Map{}, params[:i]...)
			out = append(out, params[i+1:]...)
			return append(out, p)
		}
	}
	return append(params, p)
}

// Operation returns the operation with this operationId.
func (s *Spec) Operation(id string) (Operation, bool) {
	for _, op := range s.Operations() {
		if op.ID == id {
			return op, true
		}
	}
	return Operation{}, false
}

// SchemaNames lists the component schemas in document order.
func (s *Spec) SchemaNames() []string {
	if m := Map(Map(s.Doc, "components"), "schemas"); m != nil {
		return m.Keys
	}
	return nil
}

// Component returns the component schema called name, or nil.
func (s *Spec) Component(name string) *yaml.Map {
	return Map(Map(Map(s.Doc, "components"), "schemas"), name)
}

// RefName returns the component name a {$ref: '#/components/schemas/X'} points at, or "".
func RefName(v any) string {
	m, ok := v.(*yaml.Map)
	if !ok {
		return ""
	}
	ref := Str(m, "$ref")
	if name, ok := strings.CutPrefix(ref, "#/components/schemas/"); ok {
		return name
	}
	return ""
}

// Deref follows $ref (to schemas, parameters, responses or request bodies) until it reaches a value that is not a
// reference.
func (s *Spec) Deref(v any) any {
	for i := 0; i < 32; i++ {
		m, ok := v.(*yaml.Map)
		if !ok {
			return v
		}
		ref := Str(m, "$ref")
		if ref == "" {
			return v
		}
		v = s.pointer(ref)
	}
	return v
}

// pointer resolves a local JSON pointer such as #/components/schemas/Project.
func (s *Spec) pointer(ref string) any {
	rest, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return nil
	}
	var cur any = s.Doc
	for _, part := range strings.Split(rest, "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		m, ok := cur.(*yaml.Map)
		if !ok {
			return nil
		}
		cur = get(m, part)
	}
	return cur
}

// Refs returns every component schema reachable from v, sorted.
func (s *Spec) Refs(v any) []string {
	seen := map[string]bool{}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case *yaml.Map:
			if name := RefName(t); name != "" {
				if !seen[name] {
					seen[name] = true
					walk(s.Component(name))
				}
				return
			}
			for _, k := range t.Keys {
				walk(t.Values[k])
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Helpers for reading the parsed document.

func get(m *yaml.Map, key string) any {
	if m == nil {
		return nil
	}
	return m.Values[key]
}

// Map returns m[key] as a mapping, or nil.
func Map(m *yaml.Map, key string) *yaml.Map {
	v, _ := get(m, key).(*yaml.Map)
	return v
}

// List returns m[key] as a sequence, or nil.
func List(m *yaml.Map, key string) []any {
	v, _ := get(m, key).([]any)
	return v
}

// Str returns m[key] as a string, or "".
func Str(m *yaml.Map, key string) string {
	v, _ := get(m, key).(string)
	return v
}

// Bool returns m[key] as a bool.
func Bool(m *yaml.Map, key string) bool {
	v, _ := get(m, key).(bool)
	return v
}

// Has reports whether m has key.
func Has(m *yaml.Map, key string) bool {
	if m == nil {
		return false
	}
	_, ok := m.Values[key]
	return ok
}

// Get returns m[key].
func Get(m *yaml.Map, key string) any { return get(m, key) }

// Types returns a schema's "type" as a list ("string", or ["string", "null"]).
func Types(m *yaml.Map) []string {
	switch t := get(m, "type").(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			out = append(out, fmt.Sprint(e))
		}
		return out
	}
	return nil
}

// Required returns a schema's "required" list.
func Required(m *yaml.Map) []string {
	var out []string
	for _, e := range List(m, "required") {
		out = append(out, fmt.Sprint(e))
	}
	return out
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}
