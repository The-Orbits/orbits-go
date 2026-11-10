package orbits

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLive runs against a real Orbits API and a real team. It is skipped unless ORBITS_E2E_TOKEN is set:
//
//	ORBITS_E2E_TOKEN=orb_live_… ORBITS_E2E_TEAM=<team slug> go test -run TestLive -v ./
//
// ORBITS_API_URL picks the API (staging, https://stagingapi.theorbits.app/v1, by default). The token needs full
// access to the team. The test creates its own upload project, deploys a one-page site to it, exercises the
// project's services, and deletes the project at the end, even when a step fails.
func TestLive(t *testing.T) {
	token := os.Getenv("ORBITS_E2E_TOKEN")
	if token == "" {
		t.Skip("set ORBITS_E2E_TOKEN (and ORBITS_E2E_TEAM) to run against a real API")
	}
	base := os.Getenv(EnvAPIURL)
	if base == "" {
		base = "https://stagingapi.theorbits.app/v1"
	}
	c, err := New(token, WithBaseURL(base), WithUserAgent("orbits-go-e2e/"+Version))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	me, err := c.Me.Get(ctx)
	if err != nil {
		t.Fatalf("Me.Get: %v", err)
	}
	team := os.Getenv("ORBITS_E2E_TEAM")
	if team == "" {
		if len(me.Teams) == 0 {
			t.Fatal("the token's person has no team: set ORBITS_E2E_TEAM")
		}
		team = me.Teams[0].Slug
	}
	if _, err := c.Teams.Get(ctx, team); err != nil {
		t.Fatalf("Teams.Get(%s): %v", team, err)
	}

	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	name := "orbits-go-e2e-" + hex.EncodeToString(suffix)
	kind := ProjectSourceKindUpload
	project, err := c.Projects.Create(ctx, team, ProjectCreate{
		Name:       name,
		SourceKind: &kind,
		Workload:   &Workload{Type: WorkloadTypeStatic},
	})
	if err != nil {
		t.Fatalf("Projects.Create: %v", err)
	}
	t.Logf("created project %s (%s)", project.Slug, project.ID)
	t.Cleanup(func() {
		cctx, done := context.WithTimeout(context.Background(), 3*time.Minute)
		defer done()
		op, err := c.Projects.Delete(cctx, team, project.ID)
		if err != nil {
			t.Errorf("Projects.Delete: %v", err)
			return
		}
		if _, err := c.Operations.Wait(cctx, op.ID, &WaitOptions{Timeout: 2 * time.Minute}); err != nil {
			t.Errorf("waiting for the project's deletion: %v", err)
		}
		if _, err := c.Projects.Get(cctx, team, project.ID); !IsNotFound(err) {
			t.Errorf("the project is still there after deletion: %v", err)
		}
	})

	t.Run("projects", func(t *testing.T) {
		got, err := c.Projects.Get(ctx, team, project.Slug)
		if err != nil || got.ID != project.ID {
			t.Fatalf("Projects.Get: %v", err)
		}
		found := false
		for p, err := range c.Projects.All(ctx, team, &ListProjectsParams{Q: name}) {
			if err != nil {
				t.Fatal(err)
			}
			found = found || p.ID == project.ID
		}
		if !found {
			t.Error("Projects.All does not list the new project")
		}
		if _, err := c.Projects.GetDeletionImpact(ctx, team, project.ID); err != nil {
			t.Errorf("GetDeletionImpact: %v", err)
		}
	})

	t.Run("env", func(t *testing.T) {
		res, err := c.Env.Create(ctx, team, project.ID, EnvVarCreate{Variables: []EnvVarInput{
			{Key: "ORBITS_GO_E2E", Value: "first", Sensitive: Ptr(false), Environments: []EnvironmentName{EnvironmentNameProduction, EnvironmentNameDevelopment}},
		}})
		if err != nil || res.Created != 1 {
			t.Fatalf("Env.Create: %v", err)
		}
		if _, err := c.Env.Update(ctx, team, project.ID, "ORBITS_GO_E2E", EnvVarUpdate{Value: Ptr("second")}); err != nil {
			t.Errorf("Env.Update: %v", err)
		}
		if v, err := c.Env.Reveal(ctx, team, project.ID, "ORBITS_GO_E2E"); err != nil || v.Value != "second" {
			t.Errorf("Env.Reveal: %v", err)
		}
		if pull, err := c.Env.Pull(ctx, team, project.ID, EnvironmentNameDevelopment); err != nil {
			t.Errorf("Env.Pull: %v", err)
		} else if pull.Variables["ORBITS_GO_E2E"] != "second" {
			t.Errorf("Env.Pull: %v", pull.Variables)
		}
		page, err := c.Env.List(ctx, team, project.ID, nil)
		if err != nil || page.Total != 1 {
			t.Errorf("Env.List: %v (%+v)", err, page)
		}
		if _, err := c.Env.Delete(ctx, team, project.ID, "ORBITS_GO_E2E"); err != nil {
			t.Fatalf("Env.Delete: %v", err)
		}
		if _, err := c.Env.Restore(ctx, team, project.ID, "ORBITS_GO_E2E"); err != nil {
			t.Errorf("Env.Restore: %v", err)
		}
	})

	t.Run("flags", func(t *testing.T) {
		flag, err := c.Flags.Create(ctx, team, project.ID, CreateFlagRequest{Key: "orbits-go-e2e", Type: FlagTypeBoolean})
		if err != nil {
			t.Fatalf("Flags.Create: %v", err)
		}
		if _, err := c.Flags.SetEnvironment(ctx, team, project.ID, flag.Key, FlagEnvironmentNamePreview, SetFlagEnvironmentRequest{On: Ptr(true)}); err != nil {
			t.Errorf("Flags.SetEnvironment: %v", err)
		}
		if got, err := c.Flags.Get(ctx, team, project.ID, flag.Key); err != nil || !got.Environments.Preview.On {
			t.Errorf("Flags.Get: %v", err)
		}
		if err := c.Flags.Delete(ctx, team, project.ID, flag.Key); err != nil {
			t.Errorf("Flags.Delete: %v", err)
		}
	})

	t.Run("deploy", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>orbits-go</title><p>orbits-go live test</p>\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		d, err := c.Deployments.UploadDir(ctx, team, project.ID, dir, nil)
		if err != nil {
			t.Fatalf("UploadDir: %v", err)
		}
		t.Logf("deployment %s queued", d.ID)
		lines := 0
		var done *LogStreamDone
		for ev, err := range c.Deployments.StreamLogs(ctx, team, project.ID, d.ID, nil) {
			if err != nil {
				t.Fatalf("StreamLogs: %v", err)
			}
			switch ev.Type {
			case LogEventLine:
				lines++
			case LogEventDone:
				done = ev.Done
			}
		}
		if done == nil {
			t.Fatal("the log stream ended without done")
		}
		final, err := c.Deployments.Wait(ctx, team, project.ID, d.ID, &WaitOptions{Timeout: 5 * time.Minute})
		var de *DeploymentError
		if errors.As(err, &de) {
			t.Fatalf("the deployment did not go live: %v", err)
		}
		if err != nil {
			t.Fatalf("Deployments.Wait: %v", err)
		}
		if final.State != DeploymentStateReady || done.State != DeploymentStateReady || lines == 0 {
			t.Errorf("state %s (stream said %s) with %d lines", final.State, done.State, lines)
		}
		page, err := c.Deployments.ListLogs(ctx, team, project.ID, d.ID, nil)
		if err != nil || !page.Done || len(page.Items) == 0 {
			t.Errorf("ListLogs: %v", err)
		}
		listed := false
		for dep, err := range c.Deployments.All(ctx, team, project.ID, nil) {
			if err != nil {
				t.Fatal(err)
			}
			listed = listed || dep.ID == d.ID
		}
		if !listed {
			t.Error("Deployments.All does not list the deployment")
		}
	})

	t.Run("reads", func(t *testing.T) {
		checks := map[string]func() error{
			"Templates.List":     func() error { _, err := c.Templates.List(ctx); return err },
			"Billing.ListPlans":  func() error { _, err := c.Billing.ListPlans(ctx); return err },
			"Billing.Get":        func() error { _, err := c.Billing.Get(ctx, team); return err },
			"Integrations.List":  func() error { _, err := c.Integrations.List(ctx, team); return err },
			"Domains.List":       func() error { _, err := c.Domains.List(ctx, team, nil); return err },
			"Alerts.List":        func() error { _, err := c.Alerts.List(ctx, team, project.ID); return err },
			"Workflows.List":     func() error { _, err := c.Workflows.List(ctx, team, project.ID); return err },
			"Usage.Get":          func() error { _, err := c.Usage.Get(ctx, team, nil); return err },
			"Activity.List":      func() error { _, err := c.Activity.List(ctx, team, &ListActivityParams{Limit: 5}); return err },
			"Teams.GetOverview":  func() error { _, err := c.Teams.GetOverview(ctx, team, nil); return err },
			"Observability.Get":  func() error { _, err := c.Observability.Get(ctx, team, project.ID, nil); return err },
			"Observability.Logs": func() error { _, err := c.Observability.ListLogs(ctx, team, project.ID, nil); return err },
			"Deployments.ListTeam": func() error {
				_, err := c.Deployments.ListTeam(ctx, team, &ListTeamDeploymentsParams{Limit: 5})
				return err
			},
		}
		for name, check := range checks {
			if err := check(); err != nil {
				if IsServerError(err) {
					// A store behind the endpoint may be down on a test environment: say so, but don't fail.
					t.Logf("%s: %v", name, err)
					continue
				}
				t.Errorf("%s: %v", name, err)
			}
		}
	})
}
