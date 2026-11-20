// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"context"
	"strings"
	"testing"
)

func TestCloseCopiesSummariesOfAMergedPull(t *testing.T) {
	f := syncedIssues(t, testPlan("R1", "One.", "R2", "Two."))
	f.issue(1).Labels = []string{Building}
	f.issue(2).Labels = []string{Building}
	body := BuildMarkerText(examplePlan, []string{"R1", "R2"}) + "\n" +
		SummaryBlock(examplePlan, "R1", "Feature: one.") + "\n" + SummaryBlock(examplePlan, "R2", "Feature: two.")
	pr := f.addPull(appLogin, body, "sha1")
	pr.Merged, pr.State = true, "closed"
	if err := Close(context.Background(), f.client(), appLogin, operator, pr.Number); err != nil {
		t.Fatal(err)
	}
	for n, want := range map[int]string{1: "Feature: one.", 2: "Feature: two."} {
		is := f.issue(n)
		if is.State != "closed" || is.Reason != "completed" || len(is.Labels) != 0 || is.BoardState != Done {
			t.Fatalf("issue %d = %+v", n, is)
		}
		if len(is.Comments) != 1 || !strings.Contains(is.Comments[0].Body, want) {
			t.Fatalf("issue %d comments = %v", n, is.Comments)
		}
	}
}

func TestCloseNamesAMissingSummary(t *testing.T) {
	f := syncedIssues(t, testPlan("R1", "One."))
	f.issue(1).Labels = []string{Building}
	pr := f.addPull(appLogin, BuildMarkerText(examplePlan, []string{"R1"}), "sha1")
	pr.Merged, pr.State = true, "closed"
	if err := Close(context.Background(), f.client(), appLogin, operator, pr.Number); err != nil {
		t.Fatal(err)
	}
	is := f.issue(1)
	if is.State != "closed" || !strings.Contains(is.Comments[0].Body, "no summary block") {
		t.Fatalf("issue = %+v", is)
	}
}

func TestCloseWithoutMergeBlocks(t *testing.T) {
	f := syncedIssues(t, testPlan("R1", "One."))
	f.issue(1).Labels = []string{Building}
	pr := f.addPull(appLogin, BuildMarkerText(examplePlan, []string{"R1"}), "sha1")
	pr.State = "closed"
	if err := Close(context.Background(), f.client(), appLogin, operator, pr.Number); err != nil {
		t.Fatal(err)
	}
	if is := f.issue(1); is.State != "open" || is.Labels[0] != Blocked {
		t.Fatalf("issue = %+v", is)
	}
}

func TestCloseOfAnOperatorPullWithSummaries(t *testing.T) {
	f := syncedIssues(t, testPlan("R1", "One."))
	pr := f.addPull(operator, SummaryBlock(examplePlan, "R1", "Feature: one."), "sha1")
	pr.Merged, pr.State = true, "closed"
	if err := Close(context.Background(), f.client(), appLogin, operator, pr.Number); err != nil {
		t.Fatal(err)
	}
	if is := f.issue(1); is.State != "closed" || is.Reason != "completed" {
		t.Fatalf("issue = %+v", is)
	}
}

func TestCloseIgnoresSummariesOutsideTheBuild(t *testing.T) {
	f := syncedIssues(t, testPlan("R1", "One.", "R2", "Two."))
	f.issue(1).Labels = []string{Building}
	body := BuildMarkerText(examplePlan, []string{"R1"}) + "\n" +
		SummaryBlock(examplePlan, "R1", "Feature: one.") + "\n" + SummaryBlock(examplePlan, "R2", "Feature: two.")
	pr := f.addPull(appLogin, body, "sha1")
	pr.Merged, pr.State = true, "closed"
	if err := Close(context.Background(), f.client(), appLogin, operator, pr.Number); err != nil {
		t.Fatal(err)
	}
	if f.issue(1).State != "closed" {
		t.Fatalf("issue 1 of the build must close")
	}
	if is := f.issue(2); is.State != "open" || is.Labels[0] != Proposed {
		t.Fatalf("issue 2 is outside the build and must stay proposed: %+v", is)
	}
}

func TestCloseIgnoresPullsOfOtherAuthors(t *testing.T) {
	f := syncedIssues(t, testPlan("R1", "One."))
	f.issue(1).Labels = []string{Building}
	merged := f.addPull("visitor", BuildMarkerText(examplePlan, []string{"R1"})+"\n"+SummaryBlock(examplePlan, "R1", "Feature: one."), "sha1")
	merged.Merged, merged.State = true, "closed"
	closed := f.addPull("visitor", BuildMarkerText(examplePlan, []string{"R1"}), "sha2")
	closed.State = "closed"
	for _, pr := range []*fakePull{merged, closed} {
		if err := Close(context.Background(), f.client(), appLogin, operator, pr.Number); err != nil {
			t.Fatal(err)
		}
	}
	if is := f.issue(1); is.State != "open" || is.Labels[0] != Building {
		t.Fatalf("a pull request of a visitor must change nothing: %+v", is)
	}
}

func TestCloseUsesOnlySummariesOfTheBuildPlan(t *testing.T) {
	f := syncedIssues(t, testPlan("R1", "One."))
	f.issue(1).Labels = []string{Building}
	body := BuildMarkerText(examplePlan, []string{"R1"}) + "\n" + SummaryBlock("docs/plans/other.md", "R1", "Feature: other.")
	pr := f.addPull(appLogin, body, "sha1")
	pr.Merged, pr.State = true, "closed"
	if err := Close(context.Background(), f.client(), appLogin, operator, pr.Number); err != nil {
		t.Fatal(err)
	}
	if c := f.issue(1).Comments; len(c) != 1 || !strings.Contains(c[0].Body, "no summary block") {
		t.Fatalf("comments = %v", c)
	}
}
