package orbits

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFixturesMatchSpec checks every file in testdata/fixtures against the spec: <operationId>[.label].json
// against that operation's success response, problem.<label>.json against the Problem schema. The tests answer
// with these files, so what they test is what the API documents.
func TestFixturesMatchSpec(t *testing.T) {
	spec := testSpec(t)
	files, err := filepath.Glob(filepath.Join("testdata", "fixtures", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		id, _, _ := strings.Cut(name, ".")
		var sch any
		if id == "problem" {
			sch = spec.Component("Problem")
		} else {
			op, ok := spec.Operation(id)
			if !ok {
				t.Errorf("%s: no operation %s in the spec", file, id)
				continue
			}
			success := op.Success()
			if success == nil {
				t.Errorf("%s: %s has no JSON success response", file, id)
				continue
			}
			sch = success.Schema
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if errs := spec.Validate(sch, decodeAny(t, string(data))); len(errs) > 0 {
			t.Errorf("%s does not match the spec:\n  %s", file, strings.Join(errs, "\n  "))
		}
	}
}
