// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func testPlan(reqs ...string) []Plan {
	p := Plan{Path: "docs/plans/example.md", Topic: "example"}
	for i := 0; i+1 < len(reqs); i += 2 {
		p.Requirements = append(p.Requirements, Requirement{Plan: p.Path, Topic: p.Topic, ID: reqs[i], Text: reqs[i+1], Hash: HashText(reqs[i+1])})
	}
	return []Plan{p}
}

func runSync(t *testing.T, f *fakeGitHub, plans []Plan) []string {
	t.Helper()
	var log bytes.Buffer
	reports, err := Sync(context.Background(), f.client(), plans, nil, appLogin, &log)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return reports
}

// seed runs sync once on plans and gives issue 1 the state.
func seed(t *testing.T, f *fakeGitHub, plans []Plan, state string) {
	t.Helper()
	runSync(t, f, plans)
	if state != Proposed {
		is := f.issue(1)
		is.Labels = []string{state}
		is.BoardState = state
	}
}

func TestSyncOpensOneProposedIssueForEachRequirement(t *testing.T) {
	f := newFake(t)
	runSync(t, f, testPlan("R1", "One.", "R2", "Two."))
	for n, id := range map[int]string{1: "R1", 2: "R2"} {
		is := f.issue(n)
		if is == nil || is.Title != "example "+id || len(is.Labels) != 1 || is.Labels[0] != Proposed || is.BoardState != Proposed {
			t.Fatalf("issue %d = %+v", n, is)
		}
	}
}

func TestSyncSecondRunWritesNothing(t *testing.T) {
	f := newFake(t)
	plans := testPlan("R1", "One.", "R2", "Two.")
	runSync(t, f, plans)
	before := f.writeCount()
	runSync(t, f, plans)
	if f.writeCount() != before {
		t.Fatalf("the second run wrote %d times", f.writeCount()-before)
	}
}

func TestSyncDropsARemovedReadyRequirement(t *testing.T) {
	f := newFake(t)
	seed(t, f, testPlan("R1", "One.", "R2", "Two."), Ready)
	runSync(t, f, testPlan("R2", "Two."))
	is := f.issue(1)
	if is.State != "closed" || is.Reason != "not_planned" || len(is.Labels) != 0 || is.BoardState != Dropped {
		t.Fatalf("issue = %+v", is)
	}
}

func TestSyncSetsAChangedReadyRequirementBackToProposed(t *testing.T) {
	f := newFake(t)
	seed(t, f, testPlan("R1", "One."), Ready)
	runSync(t, f, testPlan("R1", "One, changed."))
	is := f.issue(1)
	if len(is.Labels) != 1 || is.Labels[0] != Proposed {
		t.Fatalf("labels = %v", is.Labels)
	}
	if len(is.Comments) != 1 || !strings.Contains(is.Comments[0].Body, "changed") {
		t.Fatalf("comments = %v", is.Comments)
	}
	m, _ := ParseMarker(is.Body)
	if m.Hash != HashText("One, changed.") {
		t.Fatalf("the marker keeps the old hash")
	}
}

func TestSyncBlocksAChangedBuildingRequirement(t *testing.T) {
	f := newFake(t)
	seed(t, f, testPlan("R1", "One."), Building)
	runSync(t, f, testPlan("R1", "One, changed."))
	if is := f.issue(1); len(is.Labels) != 1 || is.Labels[0] != Blocked {
		t.Fatalf("labels = %v", is.Labels)
	}
}

func TestSyncBlocksARemovedBuildingRequirement(t *testing.T) {
	f := newFake(t)
	seed(t, f, testPlan("R1", "One.", "R2", "Two."), Building)
	runSync(t, f, testPlan("R2", "Two."))
	if is := f.issue(1); is.State != "open" || len(is.Labels) != 1 || is.Labels[0] != Blocked {
		t.Fatalf("issue = %+v", is)
	}
}

func TestSyncKeepsAnEditedBodyAndReportsItOnce(t *testing.T) {
	f := newFake(t)
	plans := testPlan("R1", "One.")
	runSync(t, f, plans)
	f.issue(1).Body += "\nA note of a visitor."
	edited := f.issue(1).Body
	runSync(t, f, plans)
	runSync(t, f, plans)
	is := f.issue(1)
	if is.Body != edited {
		t.Fatalf("the edited body was overwritten")
	}
	if len(is.Comments) != 1 {
		t.Fatalf("want one report comment, got %d", len(is.Comments))
	}
}

func TestSyncReportsTwoStateLabelsAndChangesNothing(t *testing.T) {
	f := newFake(t)
	seed(t, f, testPlan("R1", "One."), Ready)
	f.issue(1).Labels = []string{Ready, Blocked}
	reports := runSync(t, f, testPlan("R1", "One, changed."))
	is := f.issue(1)
	if len(reports) != 1 || len(is.Labels) != 2 {
		t.Fatalf("reports = %v, labels = %v", reports, is.Labels)
	}
}

func TestSyncIgnoresIssuesOfOtherUsers(t *testing.T) {
	f := newFake(t)
	plans := testPlan("R1", "One.")
	r := plans[0].Requirements[0]
	f.addIssue("visitor", "example R1", IssueBody(r, "opsotter/auto8"), Ready)
	runSync(t, f, plans)
	if is := f.issue(1); len(is.Labels) != 1 || is.Labels[0] != Ready || is.State != "open" {
		t.Fatalf("the issue of a visitor changed: %+v", is)
	}
	if f.issue(2) == nil {
		t.Fatalf("the sync step must open its own issue")
	}
}

func TestSyncStopsOnPlanReports(t *testing.T) {
	f := newFake(t)
	var log bytes.Buffer
	_, err := Sync(context.Background(), f.client(), nil, []string{"the topic example is in more than one plan"}, appLogin, &log)
	if err == nil || f.writeCount() != 0 {
		t.Fatalf("want an error and no writes, got %v and %d writes", err, f.writeCount())
	}
}

func TestSyncReportsAChangeAfterDone(t *testing.T) {
	f := newFake(t)
	runSync(t, f, testPlan("R1", "One."))
	is := f.issue(1)
	is.State, is.Reason, is.Labels, is.BoardState = "closed", "completed", nil, Done
	runSync(t, f, testPlan("R1", "One, changed."))
	if f.issue(2) != nil {
		t.Fatalf("a change after done must open no issue")
	}
	if len(f.issue(1).Comments) != 1 {
		t.Fatalf("want one report comment")
	}
}

func TestSyncDryRunOnThisRepository(t *testing.T) {
	f := newFake(t)
	plans, reports := LoadPlans(osDir(t), "docs/plans")
	c := f.client()
	var out bytes.Buffer
	c.DryRun, c.Out = true, &out
	if _, err := Sync(context.Background(), c, plans, reports, appLogin, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, p := range plans {
		want += len(p.Requirements)
	}
	if n := strings.Count(out.String(), `"op":"create_issue"`); want == 0 || n != want {
		t.Fatalf("want %d create lines, got %d", want, n)
	}
	if f.writeCount() != 0 {
		t.Fatalf("dry run wrote")
	}
}

func TestSyncReleasesThePlanOfAChangedBuildingRequirement(t *testing.T) {
	f := newFake(t)
	seed(t, f, testPlan("R1", "One."), Building)
	pr := f.addPull(appLogin, BuildMarkerText(examplePlan, []string{"R1"}), "sha1")
	runSync(t, f, testPlan("R1", "One, changed."))
	if f.issue(1).Labels[0] != Blocked || pr.State != "closed" {
		t.Fatalf("labels = %v, pull = %s", f.issue(1).Labels, pr.State)
	}
}

func TestSyncRenamesATopicWithoutAReport(t *testing.T) {
	f := newFake(t)
	runSync(t, f, testPlan("R1", "One."))
	renamed := testPlan("R1", "One.")
	renamed[0].Topic = "renamed"
	renamed[0].Requirements[0].Topic = "renamed"
	reports := runSync(t, f, renamed)
	is := f.issue(1)
	if len(reports) != 0 || is.Title != "renamed R1" || !strings.Contains(is.Body, "[renamed]") {
		t.Fatalf("reports = %v, issue = %+v", reports, is)
	}
	if before := f.writeCount(); len(runSync(t, f, renamed)) != 0 || f.writeCount() != before {
		t.Fatalf("a second run after the rename must write nothing")
	}
}
