package orbits

import (
	"context"
	"fmt"
	"time"
)

// WaitOptions tune a wait. The zero value (or nil) polls every 2 seconds with no limit beyond ctx's.
type WaitOptions struct {
	// Interval is the pause between polls. Default 2s.
	Interval time.Duration
	// Timeout bounds the whole wait. 0 means only ctx bounds it.
	Timeout time.Duration
}

// Done reports whether an operation has finished: succeeded or failed.
func (s OperationState) Done() bool { return s == OperationStateSucceeded || s == OperationStateFailed }

// Done reports whether a deployment has finished: ready, failed or canceled.
func (s DeploymentState) Done() bool {
	return s == DeploymentStateReady || s == DeploymentStateFailed || s == DeploymentStateCanceled
}

// OperationError is returned by Operations.Wait when the operation failed.
type OperationError struct {
	Operation *Operation
}

func (e *OperationError) Error() string {
	msg := fmt.Sprintf("orbits: operation %s (%s) failed", e.Operation.ID, e.Operation.Kind)
	if p := e.Operation.Error; p != nil {
		reason := p.Title
		if p.Detail != nil && *p.Detail != "" {
			reason = *p.Detail
		}
		if reason != "" {
			msg += ": " + reason
		}
	}
	return msg
}

// DeploymentError is returned by Deployments.Wait when the deployment failed or was canceled.
type DeploymentError struct {
	Deployment *DeploymentDetail
}

func (e *DeploymentError) Error() string {
	d := e.Deployment
	if d.State == DeploymentStateCanceled {
		return fmt.Sprintf("orbits: deployment %s was canceled", d.ID)
	}
	msg := fmt.Sprintf("orbits: deployment %s failed", d.ID)
	if f := d.Failure; f != nil {
		msg += fmt.Sprintf(" at %s: %s", f.Step, f.Title)
		if f.Message != "" {
			msg += ": " + f.Message
		}
	}
	return msg
}

// Wait polls GET /operations/{operation} until the operation succeeds, and returns it. A failed operation is
// returned with an *OperationError. ctx and opts.Timeout bound the wait; opts may be nil.
func (s *OperationsService) Wait(ctx context.Context, operationID string, opts *WaitOptions) (*Operation, error) {
	var last *Operation
	err := s.c.poll(ctx, opts, func(ctx context.Context) (bool, error) {
		op, err := s.Get(ctx, operationID)
		if err != nil {
			return false, err
		}
		last = op
		switch op.State {
		case OperationStateSucceeded:
			return true, nil
		case OperationStateFailed:
			return true, &OperationError{Operation: op}
		}
		return false, nil
	})
	return last, err
}

// Wait polls a deployment until it is ready, and returns it. A failed or canceled deployment is returned with a
// *DeploymentError, whose Failure says why. ctx and opts.Timeout bound the wait; opts may be nil. StreamLogs
// follows the same build with its log.
func (s *DeploymentsService) Wait(ctx context.Context, team, project, deployment string, opts *WaitOptions) (*DeploymentDetail, error) {
	var last *DeploymentDetail
	err := s.c.poll(ctx, opts, func(ctx context.Context) (bool, error) {
		d, err := s.Get(ctx, team, project, deployment)
		if err != nil {
			return false, err
		}
		last = d
		switch d.State {
		case DeploymentStateReady:
			return true, nil
		case DeploymentStateFailed, DeploymentStateCanceled:
			return true, &DeploymentError{Deployment: d}
		}
		return false, nil
	})
	return last, err
}

// poll runs check until it reports done or fails, sleeping between checks.
func (c *Client) poll(ctx context.Context, opts *WaitOptions, check func(context.Context) (bool, error)) error {
	interval := c.pollInterval
	if opts != nil && opts.Interval > 0 {
		interval = opts.Interval
	}
	if opts != nil && opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	for {
		done, err := check(ctx)
		if done || err != nil {
			return err
		}
		if err := c.sleep(ctx, interval); err != nil {
			return fmt.Errorf("orbits: waiting: %w", err)
		}
	}
}
