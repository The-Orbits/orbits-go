// Package gen writes the SDK's types.go and params.go from the vendored OpenAPI document: one Go type per schema
// the SDK's operations use, typed string constants for every enum, and one struct per operation's query
// parameters. scripts/gentypes runs it, and a test checks the checked-in files match its output.
//
// Mapping:
//   - A required, non-nullable property is a value; a nullable one is a pointer (nil is null); an optional one is a
//     pointer with omitempty. Slices, maps and json.RawMessage are never pointers.
//   - date-time is time.Time, integer is int (int64 for sizes in bytes), number is float64.
//   - A string enum is a named string type with a constant per value; identical value sets share one type.
//   - allOf of a struct schema embeds it; oneOf of objects is one struct with every variant's properties (those not
//     in every variant are optional); oneOf with null is a pointer.
//   - A type used in a request that has optional nullable properties gets NullFields, to send null explicitly.
package gen

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strings"

	"github.com/The-Orbits/orbits-go/internal/schema"
	"github.com/The-Orbits/orbits-go/internal/yaml"
)

type kind int

const (
	kindStruct kind = iota
	kindEnum
)

type goType struct {
	name       string
	doc        string
	kind       kind
	fields     []goField
	embeds     []string
	values     []string
	nullFields bool
}

type goField struct {
	name, json, typ, doc string
	omit                 bool
}

type generator struct {
	spec       *schema.Spec
	types      map[string]*goType
	components map[string]string // component name -> Go type expression
	building   map[string]bool
	enums      map[string]string // sorted value set -> type name
	request    map[string]bool   // component names used in request bodies
	params     []*goType
	errs       []string
	// reserved are the Go names of every component schema, which inline types may not take.
	reserved map[string]bool
	// origin is the schema each struct was built from, to catch two inline objects given one name.
	origin map[string]*yaml.Map
}

type ctx struct {
	parent    string // the Go type the value belongs to
	field     string // its JSON property name
	item      bool   // an array's items: name it in the singular
	inRequest bool
}

// Generate returns the contents of types.go and params.go.
func Generate(spec *schema.Spec) (types, params []byte, err error) {
	g := &generator{
		spec:       spec,
		types:      map[string]*goType{},
		components: map[string]string{},
		building:   map[string]bool{},
		enums:      map[string]string{},
		request:    map[string]bool{},
		reserved:   map[string]bool{},
		origin:     map[string]*yaml.Map{},
	}
	for _, name := range spec.SchemaNames() {
		g.reserved[Pascal(name)] = true
	}
	ops := Included(spec)
	for _, op := range ops {
		if op.Body != nil {
			for _, name := range spec.Refs(op.Body) {
				g.request[name] = true
			}
		}
	}
	// Component enums claim their value sets first, in document order, so inline enums with the same values share
	// their type.
	var used []string
	for _, op := range ops {
		used = append(used, spec.Refs(opSchemas(op))...)
	}
	usedSet := map[string]bool{}
	for _, u := range used {
		usedSet[u] = true
	}
	for _, name := range spec.SchemaNames() {
		if usedSet[name] && isStringEnum(spec.Component(name)) {
			g.component(name)
		}
	}
	for _, op := range ops {
		g.operation(op)
	}
	if len(g.errs) > 0 {
		return nil, nil, fmt.Errorf("gen: %s", strings.Join(g.errs, "\n"))
	}
	types, err = g.emitTypes()
	if err != nil {
		return nil, nil, err
	}
	params, err = g.emitParams()
	return types, params, err
}

// Included lists the operations the SDK covers: every one not in Skipped.
func Included(spec *schema.Spec) []schema.Operation {
	var out []schema.Operation
	for _, op := range spec.Operations() {
		if _, skip := Skipped[op.ID]; !skip {
			out = append(out, op)
		}
	}
	return out
}

func opSchemas(op schema.Operation) []any {
	var out []any
	if op.Body != nil {
		out = append(out, op.Body)
	}
	for _, r := range op.Responses {
		if r.ContentType == "application/json" && strings.HasPrefix(r.Status, "2") {
			out = append(out, r.Schema)
		}
	}
	for _, p := range op.Params {
		out = append(out, schema.Get(p, "schema"))
	}
	return out
}

func (g *generator) failf(format string, args ...any) {
	g.errs = append(g.errs, fmt.Sprintf(format, args...))
}

func (g *generator) operation(op schema.Operation) {
	if op.Body != nil {
		g.root(op.Body, "op:"+op.ID+".request", Pascal(op.ID)+"Request", true)
	}
	for _, r := range op.Responses {
		if r.ContentType == "application/json" && strings.HasPrefix(r.Status, "2") && r.Schema != nil {
			g.root(r.Schema, "op:"+op.ID+".response", Pascal(op.ID)+"Response", false)
		}
	}
	if !NoParams[op.ID] {
		g.paramsFor(op)
	}
}

// root generates the type of a request or response body.
func (g *generator) root(v any, key, fallback string, inRequest bool) {
	if schema.RefName(v) != "" {
		g.typeFor(v, ctx{inRequest: inRequest})
		return
	}
	name := fallback
	if n, ok := TypeNames[key]; ok {
		name = n
	}
	if _, done := g.types[name]; done {
		return
	}
	m, _ := v.(*yaml.Map)
	g.buildStruct(name, "", m, inRequest)
}

func isStringEnum(m *yaml.Map) bool {
	if m == nil || schema.List(m, "enum") == nil {
		return false
	}
	for _, e := range schema.List(m, "enum") {
		if _, ok := e.(string); !ok && e != nil {
			return false
		}
	}
	return true
}

// component returns the Go type for a component schema, generating it on first use.
func (g *generator) component(name string) string {
	if t, ok := g.components[name]; ok {
		return t
	}
	m := g.spec.Component(name)
	if m == nil {
		g.failf("no component schema %q", name)
		return "json.RawMessage"
	}
	goName := Pascal(name)
	switch {
	case isStringEnum(m):
		g.components[name] = goName
		g.enumType(goName, describe(m), m, true)
		return goName
	case isObjectLike(g.spec, m):
		g.components[name] = goName
		g.buildStruct(goName, describe(m), m, g.request[name])
		return goName
	}
	// A plain scalar (such as Slug, a string with a pattern) is its Go type.
	t, _ := g.typeFor(m, ctx{parent: goName})
	g.components[name] = t
	return t
}

func isObjectLike(spec *schema.Spec, m *yaml.Map) bool {
	if schema.Has(m, "properties") || schema.Has(m, "allOf") {
		return true
	}
	types := schema.Types(m)
	if len(types) > 0 && types[0] == "object" && !schema.Has(m, "additionalProperties") {
		return true
	}
	if variants := nonNull(spec, append(schema.List(m, "oneOf"), schema.List(m, "anyOf")...)); len(variants) > 1 {
		for _, v := range variants {
			if !isObjectLike(spec, deref(spec, v)) {
				return false
			}
		}
		return true
	}
	return false
}

func deref(spec *schema.Spec, v any) *yaml.Map {
	m, _ := spec.Deref(v).(*yaml.Map)
	return m
}

func isNullSchema(spec *schema.Spec, v any) bool {
	t := schema.Types(deref(spec, v))
	return len(t) == 1 && t[0] == "null"
}

func nonNull(spec *schema.Spec, variants []any) []any {
	var out []any
	for _, v := range variants {
		if !isNullSchema(spec, v) {
			out = append(out, v)
		}
	}
	return out
}

// typeFor returns the Go type of a schema and whether the schema allows null.
func (g *generator) typeFor(v any, c ctx) (string, bool) {
	if name := schema.RefName(v); name != "" {
		return g.component(name), false
	}
	m, _ := v.(*yaml.Map)
	if m == nil || len(m.Keys) == 0 {
		return "json.RawMessage", true
	}
	if all := schema.List(m, "allOf"); all != nil {
		if len(all) == 1 && !schema.Has(m, "properties") {
			return g.typeFor(all[0], c)
		}
		return g.inlineStruct(m, c), false
	}
	if variants := append(schema.List(m, "oneOf"), schema.List(m, "anyOf")...); variants != nil {
		rest := nonNull(g.spec, variants)
		nullable := len(rest) < len(variants)
		switch {
		case len(rest) == 1:
			t, n := g.typeFor(rest[0], c)
			return t, nullable || n
		case isObjectLike(g.spec, m):
			return g.inlineStruct(m, c), nullable
		}
		return "any", true
	}
	types := schema.Types(m)
	nullable := false
	var real []string
	for _, t := range types {
		if t == "null" {
			nullable = true
		} else {
			real = append(real, t)
		}
	}
	if len(real) > 1 {
		return "any", nullable
	}
	if isStringEnum(m) && (len(real) == 0 || real[0] == "string") {
		for _, e := range schema.List(m, "enum") {
			if e == nil {
				nullable = true
			}
		}
		return g.inlineEnum(m, c), nullable
	}
	t := ""
	if len(real) == 1 {
		t = real[0]
	}
	switch t {
	case "string":
		if schema.Str(m, "format") == "date-time" {
			return "time.Time", nullable
		}
		return "string", nullable
	case "integer":
		if isByteCount(c.field) {
			return "int64", nullable
		}
		return "int", nullable
	case "number":
		return "float64", nullable
	case "boolean":
		return "bool", nullable
	case "array":
		it, _ := g.typeFor(schema.Get(m, "items"), ctx{parent: c.parent, field: c.field, item: true, inRequest: c.inRequest})
		return "[]" + it, nullable
	}
	if schema.Has(m, "properties") {
		return g.inlineStruct(m, c), nullable
	}
	if schema.Has(m, "const") {
		return "string", nullable
	}
	switch ap := schema.Get(m, "additionalProperties").(type) {
	case *yaml.Map:
		vt, _ := g.typeFor(ap, ctx{parent: c.parent, field: c.field, item: true, inRequest: c.inRequest})
		return "map[string]" + vt, nullable
	case bool:
		return "map[string]any", nullable
	}
	if t == "object" {
		return "map[string]any", nullable
	}
	return "json.RawMessage", true
}

func isByteCount(field string) bool {
	return field == "size" || strings.HasSuffix(field, "_bytes")
}

// inlineName names an inline object or enum from where it is.
func inlineName(c ctx) string {
	if n, ok := TypeNames[c.parent+"."+c.field]; ok {
		return n
	}
	f := Pascal(c.field)
	if c.item {
		f = Singular(f)
	}
	return c.parent + f
}

func (g *generator) inlineStruct(m *yaml.Map, c ctx) string {
	name := inlineName(c)
	if g.reserved[name] {
		g.failf("the inline object at %s.%s would be named %s, a component's name: add a TypeNames entry", c.parent, c.field, name)
		return name
	}
	if _, ok := g.types[name]; ok {
		if g.origin[name] != m {
			g.failf("two inline objects would be named %s (the second at %s.%s): add a TypeNames entry", name, c.parent, c.field)
		}
		return name
	}
	g.origin[name] = m
	g.buildStruct(name, describe(m), m, c.inRequest)
	return name
}

func (g *generator) inlineEnum(m *yaml.Map, c ctx) string {
	key := enumKey(m)
	override, hasOverride := TypeNames[c.parent+"."+c.field]
	if !hasOverride {
		if name, ok := g.enums[key]; ok {
			return name
		}
	}
	name := inlineName(c)
	if override != "" {
		name = override
	}
	if g.reserved[name] && override == "" {
		g.failf("the inline enum at %s.%s would be named %s, a component's name: add a TypeNames entry", c.parent, c.field, name)
		return name
	}
	if t, ok := g.types[name]; ok {
		if t.kind == kindEnum && enumKeyOf(t.values) == key {
			return name
		}
		g.failf("the inline enum at %s.%s would be named %s, which is taken: add a TypeNames entry", c.parent, c.field, name)
		return name
	}
	g.enumType(name, describe(m), m, false)
	return name
}

func enumKey(m *yaml.Map) string {
	var values []string
	for _, e := range schema.List(m, "enum") {
		if s, ok := e.(string); ok {
			values = append(values, s)
		}
	}
	return enumKeyOf(values)
}

func enumKeyOf(values []string) string {
	v := append([]string{}, values...)
	sort.Strings(v)
	return strings.Join(v, "\x00")
}

func (g *generator) enumType(name, doc string, m *yaml.Map, component bool) {
	t := &goType{name: name, doc: doc, kind: kindEnum}
	for _, e := range schema.List(m, "enum") {
		if s, ok := e.(string); ok {
			t.values = append(t.values, s)
		}
	}
	g.addEnum(t)
}

func (g *generator) addEnum(t *goType) {
	if _, ok := g.types[t.name]; ok {
		g.failf("type %s generated twice", t.name)
		return
	}
	g.types[t.name] = t
	key := enumKeyOf(t.values)
	if _, ok := g.enums[key]; !ok {
		g.enums[key] = t.name
	}
}

// prop is one property of a struct being built, merged over allOf parts and oneOf variants.
type prop struct {
	json     string
	schema   any
	required bool
	consts   []any // per oneOf variant, when every variant has a const for it
	// only names the variants (by their discriminator's value) a property of some variants belongs to.
	only []string
}

// buildStruct generates a struct for an object schema, its allOf parts and its oneOf variants.
func (g *generator) buildStruct(name, doc string, m *yaml.Map, inRequest bool) {
	if m == nil {
		m = &yaml.Map{Values: map[string]any{}}
	}
	t := &goType{name: name, doc: doc, kind: kindStruct}
	g.types[name] = t
	var props []*prop
	byName := map[string]*prop{}
	add := func(p *prop) {
		if q, ok := byName[p.json]; ok {
			q.required = q.required || p.required
			return
		}
		byName[p.json] = p
		props = append(props, p)
	}
	required := map[string]bool{}
	var collect func(m *yaml.Map, embed bool)
	collect = func(m *yaml.Map, embed bool) {
		for _, r := range schema.Required(m) {
			required[r] = true
		}
		for _, part := range schema.List(m, "allOf") {
			if ref := schema.RefName(part); ref != "" && embed {
				goName := g.component(ref)
				if et, ok := g.types[goName]; ok && et.kind == kindStruct && !et.nullFields {
					t.embeds = append(t.embeds, goName)
					// The embedded struct's own required list does not apply to the fields here.
					continue
				}
			}
			collect(deref(g.spec, part), false)
		}
		if ps := schema.Map(m, "properties"); ps != nil {
			for _, k := range ps.Keys {
				add(&prop{json: k, schema: ps.Values[k]})
			}
		}
	}
	collect(m, true)
	for _, p := range props {
		p.required = p.required || required[p.json]
	}
	if variants := nonNull(g.spec, append(schema.List(m, "oneOf"), schema.List(m, "anyOf")...)); len(variants) > 1 {
		g.mergeVariants(variants, &props, byName)
	}
	for _, p := range props {
		f := g.field(t, p, inRequest)
		t.fields = append(t.fields, f)
	}
}

// mergeVariants adds every oneOf variant's properties: one is required only if it is in every variant.
func (g *generator) mergeVariants(variants []any, props *[]*prop, byName map[string]*prop) {
	type seen struct {
		p        *prop
		count    int
		required int
		consts   []any
		in       []int
	}
	order := []string{}
	all := map[string]*seen{}
	for i, v := range variants {
		vm := deref(g.spec, v)
		req := map[string]bool{}
		for _, r := range schema.Required(vm) {
			req[r] = true
		}
		ps := schema.Map(vm, "properties")
		if ps == nil {
			continue
		}
		for _, k := range ps.Keys {
			s, ok := all[k]
			if !ok {
				s = &seen{p: &prop{json: k, schema: ps.Values[k]}}
				all[k] = s
				order = append(order, k)
			}
			s.count++
			s.in = append(s.in, i)
			if req[k] {
				s.required++
			}
			if pm, _ := ps.Values[k].(*yaml.Map); pm != nil && schema.Has(pm, "const") {
				s.consts = append(s.consts, schema.Get(pm, "const"))
			}
		}
	}
	// The discriminator is the first property that is a different string constant in every variant.
	var labels []string
	for _, k := range order {
		if s := all[k]; len(s.consts) == len(variants) && allStrings(s.consts) {
			for _, c := range s.consts {
				labels = append(labels, "`"+c.(string)+"`")
			}
			break
		}
	}
	for _, k := range order {
		s := all[k]
		if _, ok := byName[k]; ok {
			continue
		}
		s.p.required = s.required == len(variants)
		if len(s.consts) == len(variants) {
			s.p.consts = s.consts
		}
		if labels != nil && s.count < len(variants) {
			for _, i := range s.in {
				s.p.only = append(s.p.only, labels[i])
			}
		}
		byName[k] = s.p
		*props = append(*props, s.p)
	}
}

func (g *generator) field(t *goType, p *prop, inRequest bool) goField {
	pm, _ := g.spec.Deref(p.schema).(*yaml.Map)
	var typ string
	var nullable bool
	switch {
	case len(p.consts) > 1 && allStrings(p.consts):
		typ = g.constEnum(t.name, p)
	case len(p.consts) > 1:
		typ = "bool"
		if _, ok := p.consts[0].(bool); !ok {
			typ = "any"
		}
	default:
		typ, nullable = g.typeFor(p.schema, ctx{parent: t.name, field: p.json, inRequest: inRequest})
	}
	name := goFieldName(t.name, p.json)
	f := goField{name: name, json: p.json, doc: fieldDoc(pm, schema.RefName(p.schema) != "")}
	if len(p.only) > 0 {
		f.doc = strings.TrimSpace("For " + strings.Join(p.only, ", ") + ". " + f.doc)
	}
	bare := strings.HasPrefix(typ, "[]") || strings.HasPrefix(typ, "map[") || typ == "any" || typ == "json.RawMessage"
	switch {
	case bare:
		f.typ = typ
		f.omit = !p.required
	case !p.required:
		f.typ = "*" + typ
		f.omit = true
	case nullable:
		f.typ = "*" + typ
	default:
		f.typ = typ
	}
	if !p.required && nullable && inRequest && typ != "json.RawMessage" {
		t.nullFields = true
	}
	return f
}

func allStrings(vs []any) bool {
	for _, v := range vs {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}

// constEnum makes the enum of a property that is a different constant in each oneOf variant, such as a
// discriminator.
func (g *generator) constEnum(parent string, p *prop) string {
	var values []string
	seen := map[string]bool{}
	for _, c := range p.consts {
		s := c.(string)
		if !seen[s] {
			seen[s] = true
			values = append(values, s)
		}
	}
	key := enumKeyOf(values)
	if name, ok := g.enums[key]; ok {
		return name
	}
	name := inlineName(ctx{parent: parent, field: p.json})
	g.addEnum(&goType{name: name, kind: kindEnum, values: values, doc: fmt.Sprintf("%s is the kind of a %s.", name, parent)})
	return name
}

func goFieldName(parent, json string) string {
	if n, ok := FieldNames[parent+"."+json]; ok {
		return n
	}
	n := Pascal(json)
	if n == "" || (n[0] >= '0' && n[0] <= '9') {
		n = "N" + n
	}
	return n
}

// keywords are the schema keys the generator knows. Any other key with a null value is the tail of a description
// cut at a comma inside a YAML flow mapping ({ description: A, B } reads as description "A" and a key "B"), and is
// put back.
var keywords = map[string]bool{
	"type": true, "description": true, "enum": true, "const": true, "format": true, "pattern": true, "items": true,
	"properties": true, "required": true, "additionalProperties": true, "allOf": true, "oneOf": true, "anyOf": true,
	"$ref": true, "default": true, "examples": true, "example": true, "minimum": true, "maximum": true,
	"exclusiveMinimum": true, "exclusiveMaximum": true, "minLength": true, "maxLength": true, "minItems": true,
	"maxItems": true, "uniqueItems": true, "readOnly": true, "writeOnly": true, "discriminator": true,
	"minProperties": true, "maxProperties": true, "title": true, "deprecated": true,
}

func describe(m *yaml.Map) string {
	if m == nil {
		return ""
	}
	d := strings.TrimSpace(schema.Str(m, "description"))
	for _, k := range m.Keys {
		if !keywords[k] && m.Values[k] == nil {
			if d != "" {
				d += ", "
			}
			d += k
		}
	}
	return d
}

func fieldDoc(m *yaml.Map, isRef bool) string {
	if m == nil {
		return ""
	}
	var parts []string
	if !isRef {
		if d := describe(m); d != "" {
			parts = append(parts, d)
		}
	}
	if enum := schema.List(m, "enum"); enum != nil && !isStringEnum(m) {
		var vs []string
		for _, e := range enum {
			vs = append(vs, fmt.Sprint(e))
		}
		parts = append(parts, "One of: "+strings.Join(vs, ", ")+".")
	}
	if schema.Has(m, "default") {
		parts = append(parts, fmt.Sprintf("Default: %s.", formatDefault(schema.Get(m, "default"))))
	}
	if schema.Bool(m, "readOnly") {
		parts = append(parts, "Read-only.")
	}
	return strings.Join(parts, " ")
}

func formatDefault(v any) string {
	switch x := v.(type) {
	case string:
		return "`" + x + "`"
	case []any:
		var parts []string
		for _, e := range x {
			parts = append(parts, fmt.Sprint(e))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return fmt.Sprint(v)
}

// paramsFor generates the query parameters struct of an operation, if it has query parameters.
func (g *generator) paramsFor(op schema.Operation) {
	var query []*yaml.Map
	for _, p := range op.Params {
		if schema.Str(p, "in") == "query" {
			query = append(query, p)
		}
	}
	if len(query) == 0 {
		return
	}
	name := Pascal(op.ID) + "Params"
	if n, ok := ParamsNames[op.ID]; ok {
		name = n
	}
	t := &goType{name: name, kind: kindStruct, doc: fmt.Sprintf("%s are the query parameters of %s %s. Zero values are left out.", name, op.Method, op.Path)}
	base := strings.TrimSuffix(name, "Params")
	for _, p := range query {
		pname := schema.Str(p, "name")
		ps := schema.Get(p, "schema")
		typ, _ := g.typeFor(ps, ctx{parent: base, field: pname})
		switch typ {
		case "bool":
			typ = "*bool"
		case "int", "string", "time.Time", "int64":
		default:
			if g.types[typ] == nil || g.types[typ].kind != kindEnum {
				g.failf("%s: query parameter %s has unsupported type %s", op.ID, pname, typ)
			}
		}
		doc := strings.TrimSpace(schema.Str(p, "description"))
		if sm := deref(g.spec, ps); sm != nil {
			if d := fieldDoc(sm, schema.RefName(ps) != ""); d != "" {
				doc = strings.TrimSpace(doc + " " + d)
			}
			if typ == "int" {
				if lo, ok := schema.Get(sm, "minimum").(int64); ok {
					if hi, ok := schema.Get(sm, "maximum").(int64); ok {
						doc = strings.TrimSpace(doc + fmt.Sprintf(" %d to %d.", lo, hi))
					}
				}
			}
		}
		if schema.Bool(p, "required") {
			doc = strings.TrimSpace("Required. " + doc)
		}
		t.fields = append(t.fields, goField{name: goFieldName(name, pname), json: pname, typ: typ, doc: doc})
	}
	g.params = append(g.params, t)
}

const header = "// Code generated by scripts/gentypes from testdata/openapi.yaml. DO NOT EDIT.\n\n"

func (g *generator) emitTypes() ([]byte, error) {
	names := make([]string, 0, len(g.types))
	for n := range g.types {
		names = append(names, n)
	}
	sort.Strings(names)
	var body bytes.Buffer
	for _, n := range names {
		t := g.types[n]
		if t.kind == kindEnum {
			emitEnum(&body, t)
		} else {
			emitStruct(&body, t)
		}
	}
	var out bytes.Buffer
	out.WriteString(header)
	out.WriteString("package orbits\n\n")
	src := body.String()
	var imports []string
	if strings.Contains(src, "json.") {
		imports = append(imports, `"encoding/json"`)
	}
	if strings.Contains(src, "time.Time") {
		imports = append(imports, `"time"`)
	}
	if len(imports) > 0 {
		out.WriteString("import (\n\t" + strings.Join(imports, "\n\t") + "\n)\n\n")
	}
	out.WriteString(src)
	return formatSource(out.Bytes())
}

func formatSource(src []byte) ([]byte, error) {
	formatted, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("gen: formatting the output: %w", err)
	}
	return formatted, nil
}

func emitEnum(b *bytes.Buffer, t *goType) {
	doc := t.doc
	if doc == "" {
		doc = fmt.Sprintf("%s is one of the %s constants.", t.name, t.name)
	} else {
		doc = t.name + ": " + doc
	}
	writeDoc(b, "", doc)
	fmt.Fprintf(b, "type %s string\n\n", t.name)
	fmt.Fprintf(b, "// %s values.\nconst (\n", t.name)
	for _, v := range t.values {
		fmt.Fprintf(b, "\t%s%s %s = %q\n", t.name, Pascal(v), t.name, v)
	}
	b.WriteString(")\n\n")
}

func emitStruct(b *bytes.Buffer, t *goType) {
	doc := t.doc
	if doc == "" {
		doc = t.name + " is part of the API's answers and requests."
	} else {
		doc = t.name + ": " + doc
	}
	writeDoc(b, "", doc)
	fmt.Fprintf(b, "type %s struct {\n", t.name)
	for _, e := range t.embeds {
		fmt.Fprintf(b, "\t%s\n", e)
	}
	for _, f := range t.fields {
		if f.doc != "" {
			writeDoc(b, "\t", f.doc)
		}
		tag := f.json
		if f.omit {
			tag += ",omitempty"
		}
		fmt.Fprintf(b, "\t%s %s `json:%q`\n", f.name, f.typ, tag)
	}
	if t.nullFields {
		writeDoc(b, "\t", "NullFields names properties, by their JSON names, to send as null. A nil pointer is left out of a request; naming its property here sends null instead, which clears the value.")
		b.WriteString("\tNullFields []string `json:\"-\"`\n")
	}
	b.WriteString("}\n\n")
	if t.nullFields {
		fmt.Fprintf(b, "// MarshalJSON sends the properties named in NullFields as null.\nfunc (v %s) MarshalJSON() ([]byte, error) {\n\ttype plain %s\n\treturn marshalWithNulls(plain(v), v.NullFields)\n}\n\n", t.name, t.name)
	}
}

// writeDoc writes text as // comments, wrapped at 110 columns.
func writeDoc(b *bytes.Buffer, indent, text string) {
	width := 110 - len(indent)*4 - 3
	for i, para := range strings.Split(strings.TrimSpace(text), "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			if i > 0 {
				b.WriteString(indent + "//\n")
			}
			continue
		}
		line := ""
		for _, w := range strings.Fields(para) {
			if line != "" && len(line)+1+len(w) > width {
				b.WriteString(indent + "// " + line + "\n")
				line = w
				continue
			}
			if line != "" {
				line += " "
			}
			line += w
		}
		if line != "" {
			b.WriteString(indent + "// " + line + "\n")
		}
	}
}

func (g *generator) emitParams() ([]byte, error) {
	sort.Slice(g.params, func(i, j int) bool { return g.params[i].name < g.params[j].name })
	var body bytes.Buffer
	for _, t := range g.params {
		writeDoc(&body, "", t.doc)
		fmt.Fprintf(&body, "type %s struct {\n", t.name)
		for _, f := range t.fields {
			if f.doc != "" {
				writeDoc(&body, "\t", f.doc)
			}
			fmt.Fprintf(&body, "\t%s %s\n", f.name, f.typ)
		}
		body.WriteString("}\n\n")
		fmt.Fprintf(&body, "func (p *%s) values() url.Values {\n\tv := url.Values{}\n\tif p == nil {\n\t\treturn v\n\t}\n", t.name)
		for _, f := range t.fields {
			switch f.typ {
			case "int":
				fmt.Fprintf(&body, "\tif p.%s != 0 {\n\t\tv.Set(%q, strconv.Itoa(p.%s))\n\t}\n", f.name, f.json, f.name)
			case "int64":
				fmt.Fprintf(&body, "\tif p.%s != 0 {\n\t\tv.Set(%q, strconv.FormatInt(p.%s, 10))\n\t}\n", f.name, f.json, f.name)
			case "*bool":
				fmt.Fprintf(&body, "\tif p.%s != nil {\n\t\tv.Set(%q, strconv.FormatBool(*p.%s))\n\t}\n", f.name, f.json, f.name)
			case "time.Time":
				fmt.Fprintf(&body, "\tif !p.%s.IsZero() {\n\t\tv.Set(%q, p.%s.UTC().Format(time.RFC3339))\n\t}\n", f.name, f.json, f.name)
			case "string":
				fmt.Fprintf(&body, "\tif p.%s != \"\" {\n\t\tv.Set(%q, p.%s)\n\t}\n", f.name, f.json, f.name)
			default: // a string enum
				fmt.Fprintf(&body, "\tif p.%s != \"\" {\n\t\tv.Set(%q, string(p.%s))\n\t}\n", f.name, f.json, f.name)
			}
		}
		body.WriteString("\treturn v\n}\n\n")
	}
	src := body.String()
	imports := []string{`"net/url"`}
	if strings.Contains(src, "strconv.") {
		imports = append(imports, `"strconv"`)
	}
	if strings.Contains(src, "time.") {
		imports = append(imports, `"time"`)
	}
	var out bytes.Buffer
	out.WriteString(header)
	out.WriteString("package orbits\n\n")
	out.WriteString("import (\n\t" + strings.Join(imports, "\n\t") + "\n)\n\n")
	out.WriteString(src)
	return formatSource(out.Bytes())
}
