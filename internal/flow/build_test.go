// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"context"
	"testing"
)

// syncedIssues runs sync on plans and returns the fake.
func syncedIssues(t *testing.T, plans []Plan) *fakeGitHub {
	t.Helper()
	f := newFake(t)
	runSync(t, f, plans)
	return f
}

// makeReady sets ready on issue n as actor, as in the web page.
func makeReady(f *fakeGitHub, n int, actor string) {
	is := f.issue(n)
	is.Labels = nil
	f.label(n, actor, Ready)
}

func twoPlans() []Plan {
	a := testPlan("R1", "One.", "R2", "Two.")
	b := Plan{Path: "docs/plans/other.md", Topic: "other"}
	b.Requirements = []Requirement{{Plan: b.Path, Topic: "other", ID: "R1", Text: "Other.", Hash: HashText("Other.")}}
	return append(a, b)
}

func TestPickTakesThePlanOfTheOldestReadyIssue(t *testing.T) {
	plans := twoPlans()
	f := syncedIssues(t, plans)
	makeReady(f, 3, operator) // other R1, oldest
	makeReady(f, 2, operator) // example R2
	makeReady(f, 1, operator) // example R1
	p, err := PickNext(context.Background(), f.client(), plans, appLogin, operator)
	if err != nil || p == nil {
		t.Fatalf("pick = %v, %v", p, err)
	}
	if p.Plan != "docs/plans/other.md" || len(p.IDs) != 1 {
		t.Fatalf("pick = %+v", p)
	}
	if s, _ := StateOf(Issue{State: "open", Labels: f.issue(3).Labels}); s != Building {
		t.Fatalf("issue 3 labels = %v", f.issue(3).Labels)
	}
	if s, _ := StateOf(Issue{State: "open", Labels: f.issue(1).Labels}); s != Ready {
		t.Fatalf("issue 1 of the other plan must stay ready")
	}
}

func TestPickTakesAllReadyIssuesOfThePlan(t *testing.T) {
	plans := twoPlans()
	f := syncedIssues(t, plans)
	makeReady(f, 1, operator)
	makeReady(f, 2, operator)
	makeReady(f, 3, operator)
	p, _ := PickNext(context.Background(), f.client(), plans, appLogin, operator)
	if p == nil || p.Plan != "docs/plans/example.md" || len(p.IDs) != 2 {
		t.Fatalf("pick = %+v", p)
	}
}

func TestPickRefusesReadyFromAnotherIdentity(t *testing.T) {
	plans := testPlan("R1", "One.")
	f := syncedIssues(t, plans)
	makeReady(f, 1, "visitor")
	p, err := PickNext(context.Background(), f.client(), plans, appLogin, operator)
	if err != nil || p != nil {
		t.Fatalf("pick = %+v, %v", p, err)
	}
}

func TestPickSkipsAPlanWithAnOpenAppPull(t *testing.T) {
	plans := testPlan("R1", "One.", "R2", "Two.")
	f := syncedIssues(t, plans)
	makeReady(f, 2, operator)
	f.addPull(appLogin, BuildMarkerText("docs/plans/example.md", []string{"R1"}), "sha1")
	if p, _ := PickNext(context.Background(), f.client(), plans, appLogin, operator); p != nil {
		t.Fatalf("pick = %+v, want nothing", p)
	}
}

func TestPickSkipsAStaleHash(t *testing.T) {
	plans := testPlan("R1", "One.")
	f := syncedIssues(t, plans)
	makeReady(f, 1, operator)
	changed := testPlan("R1", "One, changed on main.")
	if p, _ := PickNext(context.Background(), f.client(), changed, appLogin, operator); p != nil {
		t.Fatalf("pick = %+v, want nothing", p)
	}
}

func TestReapBlocksBuildingWithoutPull(t *testing.T) {
	plans := testPlan("R1", "One.", "R2", "Two.")
	f := syncedIssues(t, plans)
	f.issue(1).Labels = []string{Building}
	f.issue(2).Labels = []string{Building}
	f.addPull(appLogin, BuildMarkerText("docs/plans/example.md", []string{"R2"}), "sha1")
	changed, err := Reap(context.Background(), f.client(), appLogin)
	if err != nil || len(changed) != 1 || changed[0] != 1 {
		t.Fatalf("reap = %v, %v", changed, err)
	}
	if f.issue(2).Labels[0] != Building {
		t.Fatalf("issue 2 has an open pull request and must stay building")
	}
}

func TestMarkBlockedClosesTheAppPullAndFreesThePlan(t *testing.T) {
	plans := testPlan("R1", "One.")
	f := syncedIssues(t, plans)
	makeReady(f, 1, operator)
	c := f.client()
	if p, _ := PickNext(context.Background(), c, plans, appLogin, operator); p == nil {
		t.Fatalf("no pick")
	}
	pr := f.addPull(appLogin, BuildMarkerText("docs/plans/example.md", []string{"R1"}), "sha1")
	if err := Mark(context.Background(), c, appLogin, "docs/plans/example.md", []string{"R1"}, Blocked, "The build needs a change in chase-queue."); err != nil {
		t.Fatal(err)
	}
	if pr.State != "closed" {
		t.Fatalf("the app pull request must close")
	}
	makeReady(f, 1, operator)
	if p, _ := PickNext(context.Background(), c, plans, appLogin, operator); p == nil {
		t.Fatalf("after ready, pick must take the plan again")
	}
}

func TestMarkRefusesOtherStatesAndIssues(t *testing.T) {
	plans := testPlan("R1", "One.")
	f := syncedIssues(t, plans)
	c := f.client()
	if err := Mark(context.Background(), c, appLogin, "docs/plans/example.md", []string{"R1"}, Ready, "x"); err == nil {
		t.Fatalf("mark must refuse ready")
	}
	if err := Mark(context.Background(), c, appLogin, "docs/plans/example.md", []string{"R1"}, Blocked, "x"); err == nil {
		t.Fatalf("mark must refuse an issue that is not building")
	}
}

func TestBuildMarkerRoundTrip(t *testing.T) {
	b, ok := ParseBuildMarker("text " + BuildMarkerText("docs/plans/a.md", []string{"R1", "R12"}))
	if !ok || b.Plan != "docs/plans/a.md" || len(b.IDs) != 2 || b.IDs[1] != "R12" {
		t.Fatalf("marker = %+v %v", b, ok)
	}
}

func TestMarkNeedsOperatorLeavesThePullOpen(t *testing.T) {
	f := syncedIssues(t, testPlan("R1", "One."))
	f.issue(1).Labels = []string{Building}
	pr := f.addPull(appLogin, BuildMarkerText(examplePlan, []string{"R1"}), "sha1")
	if err := Mark(context.Background(), f.client(), appLogin, examplePlan, []string{"R1"}, NeedsOperator, "The build changes TX control."); err != nil {
		t.Fatal(err)
	}
	if f.issue(1).Labels[0] != NeedsOperator || pr.State != "open" {
		t.Fatalf("labels = %v, pull = %s", f.issue(1).Labels, pr.State)
	}
}

func TestMarkNeedsAReason(t *testing.T) {
	f := syncedIssues(t, testPlan("R1", "One."))
	f.issue(1).Labels = []string{Building}
	before := f.writeCount()
	if err := Mark(context.Background(), f.client(), appLogin, examplePlan, []string{"R1"}, Blocked, "  "); err == nil || f.writeCount() != before {
		t.Fatalf("want an error and no writes, got %v", err)
	}
}

func TestMarkBlockedLeavesThePullOfAnotherPlanOpen(t *testing.T) {
	f := syncedIssues(t, twoPlans())
	f.issue(1).Labels = []string{Building}
	mine := f.addPull(appLogin, BuildMarkerText(examplePlan, []string{"R1"}), "sha1")
	other := f.addPull(appLogin, BuildMarkerText("docs/plans/other.md", []string{"R1"}), "sha2")
	if err := Mark(context.Background(), f.client(), appLogin, examplePlan, []string{"R1"}, Blocked, "The build needs chase-queue."); err != nil {
		t.Fatal(err)
	}
	if mine.State != "closed" || other.State != "open" {
		t.Fatalf("mine = %s, other = %s", mine.State, other.State)
	}
}

func TestPickRefusesReadyLabelsAfterTheOperator(t *testing.T) {
	plans := testPlan("R1", "One.", "R2", "Two.")
	f := syncedIssues(t, plans)
	makeReady(f, 1, operator)
	f.issue(1).Labels = nil
	f.label(1, "visitor", Ready)
	f.issue(2).Labels = []string{Ready, Blocked}
	f.label(2, operator, Ready)
	f.issue(2).Labels = []string{Ready, Blocked}
	if p, _ := PickNext(context.Background(), f.client(), plans, appLogin, operator); p != nil {
		t.Fatalf("pick = %+v, want nothing", p)
	}
}
