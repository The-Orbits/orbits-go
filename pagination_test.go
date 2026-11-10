package orbits

import (
	"context"
	"errors"
	"testing"
)

func TestAllFollowsCursors(t *testing.T) {
	rec := newRecorder(t, ok(fixture(t, "listProjects.page1")), ok(fixture(t, "listProjects.page2")))
	c := newTestClient(t, rec.srv.URL)
	var slugs []string
	for p, err := range c.Projects.All(context.Background(), "acme", &ListProjectsParams{Limit: 2, Sort: ProjectSortName}) {
		if err != nil {
			t.Fatal(err)
		}
		slugs = append(slugs, p.Slug)
	}
	if len(slugs) != 3 || slugs[2] != "three" {
		t.Errorf("slugs %v", slugs)
	}
	if rec.count() != 2 {
		t.Fatalf("%d requests", rec.count())
	}
	q0, q1 := rec.request(0).URL.Query(), rec.request(1).URL.Query()
	if q0.Get("cursor") != "" || q0.Get("limit") != "2" || q0.Get("sort") != "name" {
		t.Errorf("first page query %v", q0)
	}
	if q1.Get("cursor") != "cursor_2" || q1.Get("limit") != "2" {
		t.Errorf("second page query %v", q1)
	}
}

func TestAllStopsEarly(t *testing.T) {
	rec := newRecorder(t, ok(fixture(t, "listProjects.page1")), ok(fixture(t, "listProjects.page2")))
	c := newTestClient(t, rec.srv.URL)
	for range c.Projects.All(context.Background(), "acme", nil) {
		break
	}
	if rec.count() != 1 {
		t.Errorf("%d requests after break, want 1", rec.count())
	}
}

func TestAllYieldsErrors(t *testing.T) {
	rec := newRecorder(t, ok(fixture(t, "listProjects.page1")), status(404, fixture(t, "problem.not-found")))
	c := newTestClient(t, rec.srv.URL)
	var n int
	var gotErr error
	for _, err := range c.Projects.All(context.Background(), "acme", nil) {
		if err != nil {
			gotErr = err
			break
		}
		n++
	}
	if n != 2 || !IsNotFound(gotErr) {
		t.Errorf("n = %d, err = %v", n, gotErr)
	}
}

func TestPaginateStopsOnRepeatedCursor(t *testing.T) {
	calls := 0
	next := "same"
	seq := paginate("", func(cursor string) ([]int, *string, error) {
		calls++
		if calls > 5 {
			return nil, nil, errors.New("looped")
		}
		return []int{calls}, &next, nil
	})
	for _, err := range seq {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Errorf("%d calls", calls)
	}
}

func TestParamsValues(t *testing.T) {
	live := true
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"nil", (*ListDeploymentsParams)(nil).values().Encode(), ""},
		{"zero", (&ListDeploymentsParams{}).values().Encode(), ""},
		{"set", (&ListDeploymentsParams{Limit: 10, State: DeploymentStateReady, Live: &live, Q: "fix"}).values().Encode(), "limit=10&live=true&q=fix&state=ready"},
		{"false bool", (&ListDeploymentsParams{Live: Ptr(false)}).values().Encode(), "live=false"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}
