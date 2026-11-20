// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"context"
	"fmt"
	"slices"
)

// The state labels of a requirement issue. Done and dropped are not labels:
// done is an issue closed as completed, dropped is one closed as not planned.
const (
	Proposed      = "proposed"
	Ready         = "ready"
	Building      = "building"
	Blocked       = "blocked"
	NeedsOperator = "needs-operator"

	Done    = "done"
	Dropped = "dropped"
)

// StateLabels lists the five state labels.
var StateLabels = []string{Proposed, Ready, Building, Blocked, NeedsOperator}

// IsStateLabel tells if name is one of the five state labels.
func IsStateLabel(name string) bool {
	return slices.Contains(StateLabels, name)
}

// dropStateLabels removes every state label except keep from the issue.
func (c *Client) dropStateLabels(ctx context.Context, is *Issue, keep string) error {
	var rest []string
	for _, l := range is.Labels {
		if IsStateLabel(l) && l != keep {
			if err := c.RemoveLabel(ctx, is.Number, l); err != nil {
				return err
			}
			continue
		}
		rest = append(rest, l)
	}
	is.Labels = rest
	return nil
}

// StatesOf returns the state labels of an issue.
func StatesOf(is Issue) []string {
	var out []string
	for _, l := range is.Labels {
		if IsStateLabel(l) {
			out = append(out, l)
		}
	}
	return out
}

// StateOf returns the one state of an issue. It returns ok false if the
// issue carries no state label or more than one.
func StateOf(is Issue) (string, bool) {
	if is.State == "closed" {
		return "", false
	}
	s := StatesOf(is)
	if len(s) != 1 {
		return "", false
	}
	return s[0], true
}

// SetState gives the issue exactly the one state label state. It removes
// the other state labels, writes the reason as a comment if there is one,
// and sets the State field on the project board. Every command that
// changes a state uses this function.
func (c *Client) SetState(ctx context.Context, is *Issue, state, reason string) error {
	if !IsStateLabel(state) {
		return fmt.Errorf("%s is not a state label", state)
	}
	if !is.HasLabel(state) {
		if err := c.AddLabels(ctx, is.Number, state); err != nil {
			return err
		}
		is.Labels = append(is.Labels, state)
	}
	if err := c.dropStateLabels(ctx, is, state); err != nil {
		return err
	}
	if reason != "" {
		if err := c.AddComment(ctx, is.Number, reason); err != nil {
			return err
		}
	}
	return c.SetBoardState(ctx, *is, state)
}

// Finish closes the issue as done (completed) or dropped (not planned).
// It removes all state labels first and writes the reason as a comment.
func (c *Client) Finish(ctx context.Context, is *Issue, final, reason string) error {
	var stateReason string
	switch final {
	case Done:
		stateReason = "completed"
	case Dropped:
		stateReason = "not_planned"
	default:
		return fmt.Errorf("%s is not done or dropped", final)
	}
	if err := c.dropStateLabels(ctx, is, ""); err != nil {
		return err
	}
	if reason != "" {
		if err := c.AddComment(ctx, is.Number, reason); err != nil {
			return err
		}
	}
	if err := c.CloseIssue(ctx, is.Number, stateReason); err != nil {
		return err
	}
	is.State = "closed"
	return c.SetBoardState(ctx, *is, final)
}
