// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"context"
	"strings"
	"testing"
)

// pickedTwo syncs a plan with R1 and R2 and picks both.
func pickedTwo(t *testing.T) (*fakeGitHub, Pick) {
	t.Helper()
	plans := testPlan("R1", "One.", "R2", "Two.")
	f := syncedIssues(t, plans)
	makeReady(f, 1, operator)
	makeReady(f, 2, operator)
	p, err := PickNext(context.Background(), f.client(), plans, appLogin, operator)
	if err != nil || p == nil || len(p.IDs) != 2 {
		t.Fatalf("pick = %+v, %v", p, err)
	}
	return f, *p
}

func TestParseDecisions(t *testing.T) {
	ds, err := ParseDecisions([]byte(`[{"id":"R1","state":"blocked","reason":"Needs chase-queue."}]`))
	if err != nil || len(ds) != 1 || ds[0].State != Blocked {
		t.Fatalf("decisions = %+v, %v", ds, err)
	}
	if ds, err := ParseDecisions([]byte("  \n")); err != nil || ds != nil {
		t.Fatalf("an empty file must give no decisions, got %+v, %v", ds, err)
	}
	if _, err := ParseDecisions([]byte(`[{"id":"R1","state":"blocked","reason":"x","issue":7}]`)); err == nil {
		t.Fatalf("an unknown field must fail")
	}
	if _, err := ParseDecisions([]byte(`{"id":"R1"}`)); err == nil {
		t.Fatalf("an object instead of an array must fail")
	}
}

func TestApplyRefusesADecisionForAnIssueThatTheRunDidNotPick(t *testing.T) {
	f, p := pickedTwo(t)
	before := f.writeCount()
	ds := []Decision{
		{ID: "R1", State: Blocked, Reason: "Needs chase-queue."},
		{ID: "R9", State: Blocked, Reason: "Not picked."},
	}
	if _, err := ApplyDecisions(context.Background(), f.client(), appLogin, p, ds, Blocked); err == nil {
		t.Fatalf("want an error")
	}
	if f.writeCount() != before {
		t.Fatalf("a refused list must change nothing")
	}
}

func TestApplyRefusesOtherStatesAndEmptyReasons(t *testing.T) {
	_, p := pickedTwo(t)
	for _, ds := range [][]Decision{
		{{ID: "R1", State: Ready, Reason: "x"}},
		{{ID: "R1", State: Building, Reason: "x"}},
		{{ID: "R1", State: Blocked, Reason: " "}},
		{{ID: "R1", State: Blocked, Reason: "a"}, {ID: "R1", State: NeedsOperator, Reason: "b"}},
	} {
		if err := CheckDecisions(p, ds); err == nil {
			t.Fatalf("decisions %+v must fail", ds)
		}
	}
}

func TestApplyBlockedRemovesTheIDFromThePick(t *testing.T) {
	f, p := pickedTwo(t)
	ds := []Decision{
		{ID: "R1", State: Blocked, Reason: "Needs chase-queue."},
		{ID: "R2", State: NeedsOperator, Reason: "Changes TX control."},
	}
	rest, err := ApplyDecisions(context.Background(), f.client(), appLogin, p, ds, Blocked)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.IDs) != 1 || rest.IDs[0] != "R2" || len(rest.Issues) != 1 || rest.Issues[0] != 2 {
		t.Fatalf("rest = %+v", rest)
	}
	if f.issue(1).Labels[0] != Blocked || f.issue(2).Labels[0] != Building {
		t.Fatalf("labels = %v %v", f.issue(1).Labels, f.issue(2).Labels)
	}
	if _, err := ApplyDecisions(context.Background(), f.client(), appLogin, rest, ds[1:], NeedsOperator); err != nil {
		t.Fatal(err)
	}
	if f.issue(2).Labels[0] != NeedsOperator {
		t.Fatalf("issue 2 labels = %v", f.issue(2).Labels)
	}
}

func TestPullBodyUsesThePickAndDropsForeignBlocks(t *testing.T) {
	p := Pick{Plan: examplePlan, IDs: []string{"R1", "R2"}, Issues: []int{4, 5}}
	agent := BuildMarkerText("docs/plans/other.md", []string{"R7"}) + "\n" +
		"Some text. Fixes #4, closes: opsotter/auto8#5, resolved #6.\n\n" +
		SummaryBlock(examplePlan, "R1", "Feature one.") + "\n" +
		SummaryBlock(examplePlan, "R9", "Not picked.") + "\n" +
		SummaryBlock("docs/plans/other.md", "R2", "Other plan.") + "\n"
	body := PullBody(p, agent)
	b, ok := ParseBuildMarker(body)
	if !ok || b.Plan != examplePlan || strings.Join(b.IDs, ",") != "R1,R2" {
		t.Fatalf("marker = %+v %v in\n%s", b, ok, body)
	}
	if strings.Count(body, "auto8-build") != 1 {
		t.Fatalf("the agent marker must go:\n%s", body)
	}
	s := ParseSummaries(body)
	if len(s) != 1 || s[0].ID != "R1" || s[0].Text != "Feature one." {
		t.Fatalf("summaries = %+v", s)
	}
	if !strings.Contains(body, "R1 (#4), R2 (#5)") {
		t.Fatalf("the body must name the issues:\n%s", body)
	}
	for _, kw := range []string{"Closes #", "closes #", "Fixes #", "Resolves #"} {
		if strings.Contains(body, kw) {
			t.Fatalf("the body must not use a closing keyword:\n%s", body)
		}
	}
}
