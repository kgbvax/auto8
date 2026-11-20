// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"context"
	"strings"
	"testing"
)

const examplePlan = "docs/plans/example.md"

func green(f *fakeGitHub, sha string) {
	for _, j := range RequiredJobs {
		f.checks[sha] = append(f.checks[sha], CheckRun{Name: j, Status: "completed", Conclusion: "success"})
	}
}

// buildingPull gives issue 1 (example R1) the state building and opens an App pull request for it.
func buildingPull(t *testing.T, state string) (*fakeGitHub, *fakePull) {
	t.Helper()
	f := syncedIssues(t, testPlan("R1", "One."))
	f.issue(1).Labels = []string{state}
	body := BuildMarkerText(examplePlan, []string{"R1"}) + "\n" + SummaryBlock(examplePlan, "R1", "Feature: one.\nImplementation: one file.")
	return f, f.addPull(appLogin, body, "sha1", "cmd/auto8/main.go")
}

func merge(t *testing.T, f *fakeGitHub, pr *fakePull) MergeResult {
	t.Helper()
	res, err := Merge(context.Background(), f.client(), MergeInput{App: appLogin, Operator: operator, Pull: pr.Number, Required: RequiredJobs, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestMergeMergesAGreenPull(t *testing.T) {
	f, pr := buildingPull(t, Building)
	green(f, "sha1")
	if res := merge(t, f, pr); !res.Merged || !pr.Merged {
		t.Fatalf("result = %+v", res)
	}
}

func TestMergeWaitsForUnfinishedJobs(t *testing.T) {
	f, pr := buildingPull(t, Building)
	f.checks["sha1"] = []CheckRun{{Name: "test", Status: "in_progress"}}
	if res := merge(t, f, pr); res.Merged || !strings.Contains(res.Reason, "waiting") {
		t.Fatalf("result = %+v", res)
	}
	if f.issue(1).Labels[0] != Building {
		t.Fatalf("labels = %v", f.issue(1).Labels)
	}
}

func TestMergeBlocksOnAFailedJob(t *testing.T) {
	f, pr := buildingPull(t, Building)
	green(f, "sha1")
	f.checks["sha1"][3].Conclusion = "failure"
	res := merge(t, f, pr)
	if res.Merged || pr.Merged {
		t.Fatalf("a failed job must not merge")
	}
	is := f.issue(1)
	if is.Labels[0] != Blocked || !strings.Contains(is.Comments[len(is.Comments)-1].Body, "integration") {
		t.Fatalf("issue = %+v", is)
	}
	if pr.State != "closed" {
		t.Fatalf("the pull request must close, so the plan is free again")
	}
}

func TestMergeBlocksWhenMainChangedTheRequirement(t *testing.T) {
	f, pr := buildingPull(t, Building)
	green(f, "sha1")
	head := planFS(map[string]string{"a.md": unifiedPlan})
	main := planFS(map[string]string{"a.md": strings.Replace(unifiedPlan, "first", "changed", 1)})
	res, err := Merge(context.Background(), f.client(), MergeInput{App: appLogin, Operator: operator, Pull: pr.Number, Required: RequiredJobs, Main: main, Head: head, Enabled: true})
	if err != nil || res.Merged || f.issue(1).Labels[0] != Blocked {
		t.Fatalf("result = %+v, %v, labels = %v", res, err, f.issue(1).Labels)
	}
}

func TestMergeNeverMergesAnotherAuthor(t *testing.T) {
	f := syncedIssues(t, testPlan("R1", "One."))
	pr := f.addPull("visitor", BuildMarkerText(examplePlan, []string{"R1"}), "sha1")
	green(f, "sha1")
	if res := merge(t, f, pr); res.Merged {
		t.Fatalf("result = %+v", res)
	}
}

func TestMergeWaitsForTheOperatorAndMergesAfterApproval(t *testing.T) {
	f, pr := buildingPull(t, NeedsOperator)
	green(f, "sha1")
	if res := merge(t, f, pr); res.Merged {
		t.Fatalf("a held pull request must wait")
	}
	pr.Reviews = append(pr.Reviews, Review{Author: "visitor", State: "APPROVED"})
	if res := merge(t, f, pr); res.Merged {
		t.Fatalf("only the operator releases a hold")
	}
	pr.Reviews = append(pr.Reviews, Review{Author: operator, State: "APPROVED"})
	if res := merge(t, f, pr); !res.Merged {
		t.Fatalf("result = %+v", res)
	}
}

func TestMergeBlocksWhenTheOperatorAsksForChanges(t *testing.T) {
	f, pr := buildingPull(t, NeedsOperator)
	pr.Reviews = []Review{{Author: operator, State: "CHANGES_REQUESTED"}}
	if res := merge(t, f, pr); res.Merged || f.issue(1).Labels[0] != Blocked {
		t.Fatalf("result = %+v, labels = %v", res, f.issue(1).Labels)
	}
}

func TestMergeTurnedOffStillBlocks(t *testing.T) {
	f, pr := buildingPull(t, Building)
	green(f, "sha1")
	res, _ := Merge(context.Background(), f.client(), MergeInput{App: appLogin, Operator: operator, Pull: pr.Number, Required: RequiredJobs})
	if res.Merged || !strings.Contains(res.Reason, "turned off") {
		t.Fatalf("result = %+v", res)
	}
	f.checks["sha1"][0].Conclusion = "failure"
	Merge(context.Background(), f.client(), MergeInput{App: appLogin, Operator: operator, Pull: pr.Number, Required: RequiredJobs})
	if f.issue(1).Labels[0] != Blocked {
		t.Fatalf("a failed job must block also while the merge is off")
	}
}
