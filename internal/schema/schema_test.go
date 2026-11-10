package schema

import (
	"strings"
	"testing"
)

func load(t *testing.T) *Spec {
	t.Helper()
	s, err := Load("../../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOperations(t *testing.T) {
	s := load(t)
	ops := s.Operations()
	if len(ops) < 100 {
		t.Fatalf("found %d operations", len(ops))
	}
	ids := map[string]bool{}
	for _, op := range ops {
		if op.ID == "" {
			t.Errorf("%s %s has no operationId", op.Method, op.Path)
		}
		if ids[op.ID] {
			t.Errorf("operationId %s is used twice", op.ID)
		}
		ids[op.ID] = true
	}
	op, ok := s.Operation("getProject")
	if !ok || op.Method != "GET" || op.Path != "/teams/{team}/projects/{project}" {
		t.Fatalf("getProject: %+v", op)
	}
	if op.Param("path", "team") == nil || op.Param("path", "project") == nil {
		t.Error("getProject's path parameters were not resolved")
	}
	if r := op.Success(); r == nil || RefName(r.Schema) != "Project" {
		t.Errorf("getProject's 200 is %+v", r)
	}
}

// TestExamplesMatchTheirSchemas checks the example generator against the validator, over every schema in the spec.
func TestExamplesMatchTheirSchemas(t *testing.T) {
	s := load(t)
	for _, name := range s.SchemaNames() {
		for variant := 0; variant < 4; variant++ {
			ex := s.Example(s.Component(name), variant)
			if errs := s.Validate(s.Component(name), ex); len(errs) > 0 {
				t.Errorf("%s (variant %d): %s", name, variant, strings.Join(errs, "; "))
			}
		}
	}
	for _, op := range s.Operations() {
		if op.Body != nil {
			if errs := s.Validate(op.Body, s.Example(op.Body, 0)); len(errs) > 0 {
				t.Errorf("%s request: %s", op.ID, strings.Join(errs, "; "))
			}
		}
	}
}

func TestValidate(t *testing.T) {
	s := load(t)
	project := s.Component("DeploymentProject")
	tests := []struct {
		name  string
		value any
		ok    bool
	}{
		{"valid", map[string]any{"id": "prj_1", "slug": "web", "name": "Web"}, true},
		{"missing", map[string]any{"id": "prj_1", "slug": "web"}, false},
		{"unknown", map[string]any{"id": "prj_1", "slug": "web", "name": "Web", "extra": 1.0}, false},
		{"pattern", map[string]any{"id": "dpl_1", "slug": "web", "name": "Web"}, false},
		{"type", map[string]any{"id": "prj_1", "slug": 3.0, "name": "Web"}, false},
	}
	for _, tt := range tests {
		errs := s.Validate(project, tt.value)
		if (len(errs) == 0) != tt.ok {
			t.Errorf("%s: errors %v", tt.name, errs)
		}
	}
	if errs := s.Validate(s.Component("DeploymentState"), "sleeping"); len(errs) == 0 {
		t.Error("an unknown enum value passed")
	}
}
