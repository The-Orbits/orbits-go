package schema

import (
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/The-Orbits/orbits-go/internal/yaml"
)

// alternatives expands a schema's allOf, oneOf and anyOf into the sets of schemas a value may satisfy: the value
// is valid when it satisfies every schema of one set. Each schema in a set is read without its combinators.
func (s *Spec) alternatives(v any) [][]*yaml.Map {
	m, _ := s.Deref(v).(*yaml.Map)
	if m == nil {
		return [][]*yaml.Map{{}}
	}
	alts := [][]*yaml.Map{{m}}
	for _, part := range List(m, "allOf") {
		var next [][]*yaml.Map
		for _, a := range alts {
			for _, b := range s.alternatives(part) {
				next = append(next, concat(a, b))
			}
		}
		alts = next
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		variants := List(m, key)
		if len(variants) == 0 {
			continue
		}
		var next [][]*yaml.Map
		for _, a := range alts {
			for _, variant := range variants {
				for _, b := range s.alternatives(variant) {
					next = append(next, concat(a, b))
				}
			}
		}
		alts = next
	}
	return alts
}

func concat(a, b []*yaml.Map) []*yaml.Map {
	out := make([]*yaml.Map, 0, len(a)+len(b))
	return append(append(out, a...), b...)
}

// object is the merged object view of a set of schemas.
type object struct {
	names    []string
	props    map[string][]any
	required map[string]bool
	// open: unknown properties are allowed; extra, when set, is the schema they must match.
	open  bool
	extra []any
	// constrained: some schema in the set says anything about properties.
	constrained bool
	minProps    int
}

func (s *Spec) objectView(set []*yaml.Map) object {
	o := object{props: map[string][]any{}, required: map[string]bool{}}
	for _, m := range set {
		if props := Map(m, "properties"); props != nil {
			o.constrained = true
			for _, k := range props.Keys {
				if _, ok := o.props[k]; !ok {
					o.names = append(o.names, k)
				}
				o.props[k] = append(o.props[k], props.Values[k])
			}
		}
		for _, r := range Required(m) {
			o.required[r] = true
		}
		switch ap := Get(m, "additionalProperties").(type) {
		case bool:
			o.constrained = true
			if ap {
				o.open = true
			}
		case *yaml.Map:
			o.constrained = true
			o.open = true
			o.extra = append(o.extra, ap)
		}
		if n, ok := Get(m, "minProperties").(int64); ok && int(n) > o.minProps {
			o.minProps = int(n)
		}
	}
	return o
}

// Validate checks a JSON value (as decoded by encoding/json, with or without UseNumber) against a schema and
// returns what is wrong with it. It is strict: an object property the schema does not declare is an error unless
// additionalProperties allows it.
func (s *Spec) Validate(schema any, v any) []string {
	var errs []string
	s.validate(schema, v, "$", &errs)
	return errs
}

func (s *Spec) validate(schema any, v any, path string, errs *[]string) {
	alts := s.alternatives(schema)
	var first []string
	for i, set := range alts {
		var e []string
		s.validateSet(set, v, path, &e)
		if len(e) == 0 {
			return
		}
		if i == 0 || len(e) < len(first) {
			first = e
		}
	}
	if len(alts) > 1 {
		*errs = append(*errs, fmt.Sprintf("%s: matches none of %d alternatives; closest: %s", path, len(alts), strings.Join(first, "; ")))
		return
	}
	*errs = append(*errs, first...)
}

func kindOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return "integer"
		}
		if f, err := x.Float64(); err == nil && f == math.Trunc(f) && !strings.ContainsAny(string(x), ".eE") {
			return "integer"
		}
		return "number"
	case float64:
		if x == math.Trunc(x) {
			return "integer"
		}
		return "number"
	case int, int64:
		return "integer"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func typeMatches(types []string, kind string) bool {
	for _, t := range types {
		if t == kind || (t == "number" && kind == "integer") {
			return true
		}
	}
	return false
}

func (s *Spec) validateSet(set []*yaml.Map, v any, path string, errs *[]string) {
	kind := kindOf(v)
	for _, m := range set {
		if types := Types(m); len(types) > 0 && !typeMatches(types, kind) {
			*errs = append(*errs, fmt.Sprintf("%s: is %s, want %s", path, kind, strings.Join(types, " or ")))
			return
		}
		if enum := List(m, "enum"); enum != nil && !inEnum(enum, v) {
			*errs = append(*errs, fmt.Sprintf("%s: %v is not one of %v", path, v, enum))
		}
		if Has(m, "const") && !equal(Get(m, "const"), v) {
			*errs = append(*errs, fmt.Sprintf("%s: %v is not %v", path, v, Get(m, "const")))
		}
		switch kind {
		case "string":
			for _, e := range checkString(m, v.(string)) {
				*errs = append(*errs, path+": "+e)
			}
		case "integer", "number":
			f, _ := number(v)
			if min, ok := num(m, "minimum"); ok && f < min {
				*errs = append(*errs, fmt.Sprintf("%s: %v is below the minimum %v", path, f, min))
			}
			if max, ok := num(m, "maximum"); ok && f > max {
				*errs = append(*errs, fmt.Sprintf("%s: %v is above the maximum %v", path, f, max))
			}
			if min, ok := num(m, "exclusiveMinimum"); ok && f <= min {
				*errs = append(*errs, fmt.Sprintf("%s: %v is not above %v", path, f, min))
			}
		case "array":
			arr := v.([]any)
			if n, ok := num(m, "minItems"); ok && float64(len(arr)) < n {
				*errs = append(*errs, fmt.Sprintf("%s: %d items, want at least %v", path, len(arr), n))
			}
			if n, ok := num(m, "maxItems"); ok && float64(len(arr)) > n {
				*errs = append(*errs, fmt.Sprintf("%s: %d items, want at most %v", path, len(arr), n))
			}
			if items := Get(m, "items"); items != nil {
				for i, e := range arr {
					s.validate(items, e, fmt.Sprintf("%s[%d]", path, i), errs)
				}
			}
		}
	}
	if kind != "object" {
		return
	}
	obj := v.(map[string]any)
	o := s.objectView(set)
	if !o.constrained {
		return
	}
	for _, r := range sortedKeys(o.required) {
		if _, ok := obj[r]; !ok {
			*errs = append(*errs, fmt.Sprintf("%s: missing required property %q", path, r))
		}
	}
	if len(obj) < o.minProps {
		*errs = append(*errs, fmt.Sprintf("%s: %d properties, want at least %d", path, len(obj), o.minProps))
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		schemas, ok := o.props[k]
		if !ok {
			// A required property is known even when no schema in the set declares it (an abstract base).
			if !o.open && !o.required[k] {
				*errs = append(*errs, fmt.Sprintf("%s: unknown property %q", path, k))
			}
			for _, extra := range o.extra {
				s.validate(extra, obj[k], path+"."+k, errs)
			}
			continue
		}
		for _, ps := range schemas {
			s.validate(ps, obj[k], path+"."+k, errs)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func num(m *yaml.Map, key string) (float64, bool) {
	switch x := Get(m, key).(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func equal(a, b any) bool {
	fa, oka := number(a)
	fb, okb := number(b)
	if oka && okb {
		return fa == fb
	}
	return fmt.Sprint(a) == fmt.Sprint(b) && kindOf(a) == kindOf(b)
}

func inEnum(enum []any, v any) bool {
	for _, e := range enum {
		if equal(e, v) {
			return true
		}
	}
	return false
}

var (
	reMu    sync.Mutex
	reCache = map[string]*regexp.Regexp{}
)

func pattern(p string) *regexp.Regexp {
	reMu.Lock()
	defer reMu.Unlock()
	if re, ok := reCache[p]; ok {
		return re
	}
	re, err := regexp.Compile(p)
	if err != nil {
		re = nil // a pattern Go cannot read is not checked
	}
	reCache[p] = re
	return re
}

func checkString(m *yaml.Map, v string) []string {
	var errs []string
	n := float64(utf8.RuneCountInString(v))
	if min, ok := num(m, "minLength"); ok && n < min {
		errs = append(errs, fmt.Sprintf("%q is shorter than %v", v, min))
	}
	if max, ok := num(m, "maxLength"); ok && n > max {
		errs = append(errs, fmt.Sprintf("%q is longer than %v", v, max))
	}
	if p := Str(m, "pattern"); p != "" {
		if re := pattern(p); re != nil && !re.MatchString(v) {
			errs = append(errs, fmt.Sprintf("%q does not match %s", v, p))
		}
	}
	switch Str(m, "format") {
	case "date-time":
		if _, err := time.Parse(time.RFC3339, v); err != nil {
			errs = append(errs, fmt.Sprintf("%q is not an RFC 3339 date-time", v))
		}
	case "date":
		if _, err := time.Parse("2006-01-02", v); err != nil {
			errs = append(errs, fmt.Sprintf("%q is not a date", v))
		}
	case "email":
		if _, err := mail.ParseAddress(v); err != nil {
			errs = append(errs, fmt.Sprintf("%q is not an email address", v))
		}
	case "uri":
		if u, err := url.Parse(v); err != nil || u.Scheme == "" {
			errs = append(errs, fmt.Sprintf("%q is not an absolute URI", v))
		}
	case "uri-reference":
		if _, err := url.Parse(v); err != nil {
			errs = append(errs, fmt.Sprintf("%q is not a URI reference", v))
		}
	}
	return errs
}
