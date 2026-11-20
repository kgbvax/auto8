// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
)

// RequiredJobs are the CI jobs that must pass before a merge.
var RequiredJobs = []string{"test", "writing", "requirements", "integration"}

// MergeInput holds what the merge step needs besides the client.
type MergeInput struct {
	App, Operator string
	Pull          int
	Required      []string
	Main, Head    fs.FS // the plans of the current main and of the pull request head
	Enabled       bool  // false: only the blocked results happen, no merge
}

// MergeResult says what the merge step did and why.
type MergeResult struct {
	Merged bool
	Reason string
}

// lastOperatorReview returns the last APPROVED or CHANGES_REQUESTED
// review state of operator. An approval counts only for the commit head,
// so a push after the approval needs a new approval. A change request
// counts for any commit.
func lastOperatorReview(reviews []Review, operator, head string) string {
	last := ""
	for _, r := range reviews {
		if r.Author != operator {
			continue
		}
		switch r.State {
		case "APPROVED":
			if r.CommitID == head {
				last = r.State
			} else {
				last = ""
			}
		case "CHANGES_REQUESTED":
			last = r.State
		case "DISMISSED":
			last = ""
		}
	}
	return last
}

// failedConclusions are the check run results that block the issues.
// Other results that are not success, such as skipped or neutral, only
// stop the merge.
var failedConclusions = map[string]bool{
	"failure": true, "timed_out": true, "cancelled": true, "action_required": true, "startup_failure": true,
}

// Merge merges an App pull request if all of these are true: each
// requirement of the build has an open issue, every required job passed,
// no issue of the build is blocked, an approval of the operator on the
// head commit released each needs-operator issue, and the requirements of
// the build on the head match the current main. A failed job, a change
// request of the operator, or a changed requirement of the build sets the
// issues to blocked.
func Merge(ctx context.Context, c *Client, in MergeInput) (MergeResult, error) {
	pr, err := c.GetPull(ctx, in.Pull)
	if err != nil {
		return MergeResult{}, err
	}
	if pr.Author != in.App {
		return MergeResult{Reason: "not a pull request of the App"}, nil
	}
	if pr.State != "open" {
		return MergeResult{Reason: "the pull request is not open"}, nil
	}
	if pr.BaseRef != "main" {
		return MergeResult{Reason: "the pull request does not target main"}, nil
	}
	build, ok := ParseBuildMarker(pr.Body)
	if !ok {
		return MergeResult{Reason: "the pull request has no build marker"}, nil
	}
	issues, err := openManaged(ctx, c, in.App)
	if err != nil {
		return MergeResult{}, err
	}
	list, missing := buildIssues(issues, build)
	if len(missing) > 0 {
		return MergeResult{Reason: "no open issue for " + strings.Join(missing, ", ")}, nil
	}
	block := func(reason string) (MergeResult, error) {
		return MergeResult{Reason: reason}, BlockAndRelease(ctx, c, in.App, build.Plan, list, reason)
	}

	reviews, err := c.Reviews(ctx, in.Pull)
	if err != nil {
		return MergeResult{}, err
	}
	review := lastOperatorReview(reviews, in.Operator, pr.HeadSHA)
	if review == "CHANGES_REQUESTED" {
		return block(fmt.Sprintf("The operator asked for changes on pull request #%d.", in.Pull))
	}

	runs, err := c.CheckRuns(ctx, pr.HeadSHA)
	if err != nil {
		return MergeResult{}, err
	}
	byName := map[string]CheckRun{}
	for _, r := range runs {
		byName[r.Name] = r
	}
	var waiting, notPassed []string
	for _, name := range in.Required {
		r, ok := byName[name]
		switch {
		case !ok || r.Status != "completed":
			waiting = append(waiting, name)
		case failedConclusions[r.Conclusion]:
			return block(fmt.Sprintf("The required job %s failed on pull request #%d.", name, in.Pull))
		case r.Conclusion != "success":
			notPassed = append(notPassed, name+" ("+r.Conclusion+")")
		}
	}

	if in.Main != nil && in.Head != nil {
		if diffs := CompareBuild(in.Main, in.Head, PlansDir, build); len(diffs) > 0 {
			return block(fmt.Sprintf("The requirements on main changed while pull request #%d was open: %s.", in.Pull, strings.Join(diffs, ", ")))
		}
	}
	if len(waiting) > 0 {
		return MergeResult{Reason: "waiting for the jobs " + strings.Join(waiting, ", ")}, nil
	}
	if len(notPassed) > 0 {
		return MergeResult{Reason: "required jobs did not pass: " + strings.Join(notPassed, ", ")}, nil
	}

	for _, is := range list {
		s, ok := StateOf(*is)
		switch {
		case !ok:
			return MergeResult{Reason: fmt.Sprintf("issue #%d does not have exactly one state label", is.Number)}, nil
		case s == Blocked:
			return MergeResult{Reason: fmt.Sprintf("issue #%d is blocked", is.Number)}, nil
		case s == NeedsOperator && review != "APPROVED":
			return MergeResult{Reason: fmt.Sprintf("issue #%d waits for the approval of the operator", is.Number)}, nil
		case s != Building && s != NeedsOperator:
			return MergeResult{Reason: fmt.Sprintf("issue #%d is %s, not building", is.Number, s)}, nil
		}
	}
	if !in.Enabled {
		return MergeResult{Reason: "the merge step is turned off"}, nil
	}
	if err := c.MergePull(ctx, in.Pull, pr.HeadSHA); err != nil {
		return MergeResult{}, err
	}
	return MergeResult{Merged: true, Reason: "all gates passed"}, nil
}
