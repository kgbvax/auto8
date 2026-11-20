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
	setConclusion(f, "sha1", "integration", "failure")
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
	head := planFS(map[string]string{"example.md": unifiedPlan})
	main := planFS(map[string]string{"example.md": strings.Replace(unifiedPlan, "first", "changed", 1)})
	res, err := Merge(context.Background(), f.client(), MergeInput{App: appLogin, Operator: operator, Pull: pr.Number, Required: RequiredJobs, Main: main, Head: head, Enabled: true})
	if err != nil || res.Merged || f.issue(1).Labels[0] != Blocked {
		t.Fatalf("result = %+v, %v, labels = %v", res, err, f.issue(1).Labels)
	}
}

func TestMergeNeverMergesAnotherAuthor(t *testing.T) {
	f := syncedIssues(t, testPlan("R1", "One."))
	f.issue(1).Labels = []string{Building}
	pr := f.addPull("visitor", BuildMarkerText(examplePlan, []string{"R1"}), "sha1")
	green(f, "sha1")
	if res := merge(t, f, pr); res.Merged || pr.Merged || !strings.Contains(res.Reason, "not a pull request of the App") {
		t.Fatalf("result = %+v", res)
	}
}

func TestMergeWaitsForTheOperatorAndMergesAfterApproval(t *testing.T) {
	f, pr := buildingPull(t, NeedsOperator)
	green(f, "sha1")
	if res := merge(t, f, pr); res.Merged {
		t.Fatalf("a held pull request must wait")
	}
	pr.Reviews = append(pr.Reviews, Review{Author: "visitor", State: "APPROVED", CommitID: "sha1"})
	if res := merge(t, f, pr); res.Merged {
		t.Fatalf("only the operator releases a hold")
	}
	pr.Reviews = append(pr.Reviews, Review{Author: operator, State: "APPROVED", CommitID: "sha1"})
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
	setConclusion(f, "sha1", "test", "failure")
	Merge(context.Background(), f.client(), MergeInput{App: appLogin, Operator: operator, Pull: pr.Number, Required: RequiredJobs})
	if f.issue(1).Labels[0] != Blocked {
		t.Fatalf("a failed job must block also while the merge is off")
	}
}

func setConclusion(f *fakeGitHub, sha, job, conclusion string) {
	for i := range f.checks[sha] {
		if f.checks[sha][i].Name == job {
			f.checks[sha][i].Conclusion = conclusion
		}
	}
}

func TestMergeRefusesABuildWithoutOpenIssues(t *testing.T) {
	f, pr := buildingPull(t, Building)
	green(f, "sha1")
	is := f.issue(1)
	is.State, is.Reason, is.Labels = "closed", "not_planned", nil
	if res := merge(t, f, pr); res.Merged || !strings.Contains(res.Reason, "no open issue") {
		t.Fatalf("result = %+v", res)
	}
}

func TestMergeRefusesAMarkerWithAnUnknownID(t *testing.T) {
	f := syncedIssues(t, testPlan("R1", "One."))
	f.issue(1).Labels = []string{Building}
	pr := f.addPull(appLogin, BuildMarkerText(examplePlan, []string{"R1", "R9"}), "sha1")
	green(f, "sha1")
	if res := merge(t, f, pr); res.Merged || !strings.Contains(res.Reason, "R9") {
		t.Fatalf("result = %+v", res)
	}
}

func TestMergeRefusesUnsafePulls(t *testing.T) {
	for name, setup := range map[string]func(f *fakeGitHub, pr *fakePull){
		"no build marker":   func(f *fakeGitHub, pr *fakePull) { pr.Body = "no marker" },
		"closed":            func(f *fakeGitHub, pr *fakePull) { pr.State = "closed" },
		"issue is ready":    func(f *fakeGitHub, pr *fakePull) { f.issue(1).Labels = []string{Ready} },
		"issue is proposed": func(f *fakeGitHub, pr *fakePull) { f.issue(1).Labels = []string{Proposed} },
		"two state labels":  func(f *fakeGitHub, pr *fakePull) { f.issue(1).Labels = []string{Building, Blocked} },
		"stale approval": func(f *fakeGitHub, pr *fakePull) {
			f.issue(1).Labels = []string{NeedsOperator}
			pr.Reviews = []Review{{Author: operator, State: "APPROVED", CommitID: "old"}}
		},
		"approval then dismiss": func(f *fakeGitHub, pr *fakePull) {
			f.issue(1).Labels = []string{NeedsOperator}
			pr.Reviews = []Review{{Author: operator, State: "APPROVED", CommitID: "sha1"}, {Author: operator, State: "DISMISSED"}}
		},
		"skipped job": func(f *fakeGitHub, pr *fakePull) { setConclusion(f, "sha1", "integration", "skipped") },
	} {
		t.Run(name, func(t *testing.T) {
			f, pr := buildingPull(t, Building)
			green(f, "sha1")
			setup(f, pr)
			if res := merge(t, f, pr); res.Merged || pr.Merged {
				t.Fatalf("result = %+v", res)
			}
		})
	}
}

func TestMergeSkippedJobDoesNotBlock(t *testing.T) {
	f, pr := buildingPull(t, Building)
	green(f, "sha1")
	setConclusion(f, "sha1", "integration", "skipped")
	merge(t, f, pr)
	if f.issue(1).Labels[0] != Building || pr.State != "open" {
		t.Fatalf("a skipped job must only stop the merge, labels = %v, pull = %s", f.issue(1).Labels, pr.State)
	}
}

func TestMergeReviewOrder(t *testing.T) {
	approve := Review{Author: operator, State: "APPROVED", CommitID: "sha1"}
	changes := Review{Author: operator, State: "CHANGES_REQUESTED", CommitID: "sha1"}

	f, pr := buildingPull(t, NeedsOperator)
	green(f, "sha1")
	pr.Reviews = []Review{approve, changes}
	if res := merge(t, f, pr); res.Merged || f.issue(1).Labels[0] != Blocked {
		t.Fatalf("approval then change request: result = %+v, labels = %v", res, f.issue(1).Labels)
	}

	f, pr = buildingPull(t, NeedsOperator)
	green(f, "sha1")
	pr.Reviews = []Review{changes, approve}
	if res := merge(t, f, pr); !res.Merged {
		t.Fatalf("change request then approval: result = %+v", res)
	}
}

func TestMergeIgnoresChangesToOtherRequirements(t *testing.T) {
	f, pr := buildingPull(t, Building)
	green(f, "sha1")
	head := planFS(map[string]string{"example.md": unifiedPlan})
	main := planFS(map[string]string{
		"example.md": strings.Replace(unifiedPlan, "The third requirement.", "The third requirement, changed.", 1),
		"other.md":   strings.Replace(unifiedPlan, "topic: example", "topic: other", 1),
	})
	res, err := Merge(context.Background(), f.client(), MergeInput{App: appLogin, Operator: operator, Pull: pr.Number, Required: RequiredJobs, Main: main, Head: head, Enabled: true})
	if err != nil || !res.Merged {
		t.Fatalf("a change outside the build must not stop the merge: %+v, %v", res, err)
	}
}
