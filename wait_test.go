package orbits

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOperationsWait(t *testing.T) {
	tests := []struct {
		name      string
		responses []canned
		wantState OperationState
		wantErr   string
	}{
		{"succeeds", []canned{ok(fixture(t, "getOperation.running")), ok(fixture(t, "getOperation.succeeded"))}, OperationStateSucceeded, ""},
		{"fails", []canned{ok(fixture(t, "getOperation.running")), ok(fixture(t, "getOperation.failed"))}, OperationStateFailed, "could not be detached"},
		{"not found", []canned{status(404, fixture(t, "problem.not-found"))}, "", "Project not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newRecorder(t, tt.responses...)
			c := newTestClient(t, rec.srv.URL)
			op, err := c.Operations.Wait(context.Background(), "op_test1", nil)
			if tt.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			if tt.wantState != "" && (op == nil || op.State != tt.wantState) {
				t.Errorf("op = %+v", op)
			}
			if tt.wantState == OperationStateFailed {
				var oe *OperationError
				if !errors.As(err, &oe) || oe.Operation.Error == nil {
					t.Errorf("err = %T", err)
				}
			}
		})
	}
}

func TestDeploymentsWait(t *testing.T) {
	tests := []struct {
		name      string
		final     string
		wantState DeploymentState
		wantErr   string
	}{
		{"ready", "getDeployment.ready", DeploymentStateReady, ""},
		{"failed", "getDeployment.failed", DeploymentStateFailed, "failed at building: Build failed: npm run build exited with code 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newRecorder(t, ok(fixture(t, "getDeployment.building")), ok(fixture(t, "getDeployment.building")), ok(fixture(t, tt.final)))
			c := newTestClient(t, rec.srv.URL)
			d, err := c.Deployments.Wait(context.Background(), "acme", "web", "dpl_test1", &WaitOptions{Interval: time.Millisecond})
			if tt.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if tt.wantErr != "" {
				var de *DeploymentError
				if !errors.As(err, &de) || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v", err)
				}
			}
			if d.State != tt.wantState || rec.count() != 3 {
				t.Errorf("state %s after %d polls", d.State, rec.count())
			}
			if got := rec.request(0).URL.Path; got != "/v1/teams/acme/projects/web/deployments/dpl_test1" {
				t.Errorf("polled %s", got)
			}
		})
	}
}

func TestWaitTimeout(t *testing.T) {
	rec := newRecorder(t, ok(fixture(t, "getDeployment.building")))
	c := newTestClient(t, rec.srv.URL)
	c.sleep = sleepContext
	_, err := c.Deployments.Wait(context.Background(), "acme", "web", "dpl_test1", &WaitOptions{Interval: 5 * time.Millisecond, Timeout: 30 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
}

func TestStateDone(t *testing.T) {
	for s, want := range map[DeploymentState]bool{DeploymentStateQueued: false, DeploymentStateBuilding: false, DeploymentStateReady: true, DeploymentStateFailed: true, DeploymentStateCanceled: true} {
		if s.Done() != want {
			t.Errorf("%s.Done() = %v", s, !want)
		}
	}
	if OperationStateRunning.Done() || !OperationStateFailed.Done() {
		t.Error("OperationState.Done")
	}
}
