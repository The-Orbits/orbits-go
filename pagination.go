package orbits

import (
	"encoding/json"
	"iter"
)

// paginate yields every item of every page, starting at cursor first. fetch returns a page's items and its
// next_cursor. A cursor that repeats ends the loop, so a server bug cannot spin it forever.
func paginate[T any](first string, fetch func(cursor string) ([]T, *string, error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		cursor := first
		seen := map[string]bool{}
		for {
			items, next, err := fetch(cursor)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}
			seen[cursor] = true
			if next == nil || *next == "" || seen[*next] {
				return
			}
			cursor = *next
		}
	}
}

// Ptr returns a pointer to v. Optional request fields are pointers, so a field left nil is not sent, and Ptr sets
// one inline:
//
//	c.Projects.Update(ctx, "acme", "web", orbits.ProjectUpdate{Name: orbits.Ptr("Storefront")})
func Ptr[T any](v T) *T { return &v }

// marshalWithNulls encodes v, then sets each named property to null. Generated MarshalJSON methods use it for
// NullFields.
func marshalWithNulls(v any, nulls []string) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil || len(nulls) == 0 {
		return data, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	for _, k := range nulls {
		m[k] = json.RawMessage("null")
	}
	return json.Marshal(m)
}
