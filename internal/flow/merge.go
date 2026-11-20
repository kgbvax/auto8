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
// review state of operator, or an empty string.
func lastOperatorReview(reviews []Review, operator string) string {
	last := ""
	for _, r := range reviews {
		if r.Author != operator {
			continue
		}
		switch r.State {
		case "APPROVED", "CHANGES_REQUESTED":
			last = r.State
		case "DISMISSED":
			last = ""
		}
	}
	return last
}

// Merge merges an App pull request if all of these are true: every
// required job passed, no issue of the build is blocked, needs-operator
// is released by an approval of the operator, and the requirements of
// the head match the current main. A failed job, a change request of
// the operator, or a changed requirement sets the issues to blocked.
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
	build, ok := ParseBuildMarker(pr.Body)
	if !ok {
		return MergeResult{Reason: "the pull request has no build marker"}, nil
	}
	issues, err := openManaged(ctx, c, in.App)
	if err != nil {
		return MergeResult{}, err
	}
	var list []*Issue
	for _, id := range build.IDs {
		if is, ok := issues[key(build.Plan, id)]; ok {
			is := is
			list = append(list, &is)
		}
	}
	block := func(reason string) (MergeResult, error) {
		return MergeResult{Reason: reason}, BlockAndRelease(ctx, c, in.App, build.Plan, list, reason)
	}

	reviews, err := c.Reviews(ctx, in.Pull)
	if err != nil {
		return MergeResult{}, err
	}
	review := lastOperatorReview(reviews, in.Operator)
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
	var waiting []string
	for _, name := range in.Required {
		r, ok := byName[name]
		if !ok || r.Status != "completed" {
			waiting = append(waiting, name)
			continue
		}
		if r.Conclusion != "success" {
			return block(fmt.Sprintf("The required job %s failed on pull request #%d.", name, in.Pull))
		}
	}

	if in.Main != nil && in.Head != nil {
		if diffs := CompareRequirements(in.Main, in.Head, PlansDir); len(diffs) > 0 {
			return block(fmt.Sprintf("The requirements on main changed while pull request #%d was open: %s.", in.Pull, strings.Join(diffs, ", ")))
		}
	}
	if len(waiting) > 0 {
		return MergeResult{Reason: "waiting for the jobs " + strings.Join(waiting, ", ")}, nil
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
