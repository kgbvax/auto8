// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"context"
	"testing"
)

func TestSetStateReplacesTheOldStateLabel(t *testing.T) {
	f := newFake(t)
	f.addIssue(appLogin, "t R1", "b", Ready, "other")
	c := f.client()
	is, err := c.GetIssue(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetState(context.Background(), &is, Blocked, "The build stopped."); err != nil {
		t.Fatal(err)
	}
	got := f.issue(1)
	if s, ok := StateOf(Issue{Labels: got.Labels}); !ok || s != Blocked {
		t.Fatalf("labels = %v, want only the state blocked", got.Labels)
	}
	if !(Issue{Labels: got.Labels}).HasLabel("other") {
		t.Fatalf("a label that is not a state label was removed: %v", got.Labels)
	}
	if len(got.Comments) != 1 || got.Comments[0].Body != "The build stopped." {
		t.Fatalf("comments = %v", got.Comments)
	}
	if got.BoardState != Blocked {
		t.Fatalf("board state = %q, want blocked", got.BoardState)
	}
}

func TestTwoStateLabelsGiveNoState(t *testing.T) {
	if _, ok := StateOf(Issue{State: "open", Labels: []string{Ready, Blocked}}); ok {
		t.Fatalf("two state labels must give no state")
	}
	if s, ok := StateOf(Issue{State: "open", Labels: []string{"x", Ready}}); !ok || s != Ready {
		t.Fatalf("want ready, got %q %v", s, ok)
	}
}

func TestFinishClosesAndClearsStates(t *testing.T) {
	f := newFake(t)
	f.addIssue(appLogin, "t R1", "b", Ready)
	c := f.client()
	is, _ := c.GetIssue(context.Background(), 1)
	if err := c.Finish(context.Background(), &is, Dropped, "R1 left the plan."); err != nil {
		t.Fatal(err)
	}
	got := f.issue(1)
	if got.State != "closed" || got.Reason != "not_planned" || len(got.Labels) != 0 || got.BoardState != Dropped {
		t.Fatalf("issue = %+v", got)
	}
}

func TestSetStateRefusesAnUnknownState(t *testing.T) {
	c := &Client{}
	if err := c.SetState(context.Background(), &Issue{}, "merged", ""); err == nil {
		t.Fatalf("want an error for an unknown state")
	}
}
