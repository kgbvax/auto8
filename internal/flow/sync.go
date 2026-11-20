// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"context"
	"fmt"
	"io"
	"sort"
)

// key names one requirement across all plans.
func key(plan, id string) string { return plan + "#" + id }

// managedIssues returns the issues that app opened and that carry a
// marker, by requirement. Each list is sorted by issue number.
func managedIssues(all []Issue, app string) map[string][]Issue {
	out := map[string][]Issue{}
	for _, is := range all {
		if is.Author != app {
			continue
		}
		m, ok := ParseMarker(is.Body)
		if !ok {
			continue
		}
		k := key(m.Plan, m.ID)
		out[k] = append(out[k], is)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i].Number < out[k][j].Number })
	}
	return out
}

// firstOpen returns the open issue with the lowest number in list.
func firstOpen(list []Issue) (Issue, bool) {
	for _, is := range list {
		if is.State == "open" {
			return is, true
		}
	}
	return Issue{}, false
}

// Reporter writes a report about one issue as a comment, once.
type Reporter struct {
	C       *Client
	App     string
	Out     io.Writer
	Reports []string
}

// Report writes text on the issue as a comment, if the app did not
// already write the same text there.
func (r *Reporter) Report(ctx context.Context, n int, text string) error {
	line := fmt.Sprintf("#%d: %s", n, text)
	r.Reports = append(r.Reports, line)
	if r.Out != nil {
		fmt.Fprintln(r.Out, line)
	}
	comments, err := r.C.Comments(ctx, n)
	if err != nil {
		return err
	}
	for _, cm := range comments {
		if cm.Author == r.App && cm.Body == text {
			return nil
		}
	}
	return r.C.AddComment(ctx, n, text)
}

// finalState returns the state that the board must show for the issue.
func finalState(is Issue) (string, bool) {
	if is.State == "closed" {
		if is.Reason == "not_planned" {
			return Dropped, true
		}
		return Done, true
	}
	return StateOf(is)
}

// Sync makes the issues match the plans. loadReports are the reports of
// LoadPlans. If there are any, Sync changes nothing and returns an error,
// because a plan that did not load would look like removed requirements.
// app is the login of the GitHub App. log gets one line for each report.
func Sync(ctx context.Context, c *Client, plans []Plan, loadReports []string, app string, log io.Writer) ([]string, error) {
	if len(loadReports) > 0 {
		for _, r := range loadReports {
			fmt.Fprintln(log, r)
		}
		return loadReports, fmt.Errorf("sync stopped: %d problems in the plan files", len(loadReports))
	}
	all, err := c.ListIssues(ctx)
	if err != nil {
		return nil, err
	}
	issues := managedIssues(all, app)
	rep := &Reporter{C: c, App: app, Out: log}
	want := map[string]bool{}

	for _, p := range plans {
		for _, r := range p.Requirements {
			k := key(r.Plan, r.ID)
			want[k] = true
			if err := syncOne(ctx, c, rep, r, issues[k]); err != nil {
				return rep.Reports, err
			}
		}
	}

	var gone []string
	for k := range issues {
		if !want[k] {
			gone = append(gone, k)
		}
	}
	sort.Strings(gone)
	for _, k := range gone {
		for _, is := range issues[k] {
			if is.State != "open" {
				continue
			}
			m, _ := ParseMarker(is.Body)
			if err := syncGone(ctx, c, rep, is, m); err != nil {
				return rep.Reports, err
			}
		}
	}
	return rep.Reports, nil
}

// syncBoard sets the State field of the issue on the board if it differs.
func syncBoard(ctx context.Context, c *Client, is Issue) error {
	s, ok := finalState(is)
	if !ok {
		return nil
	}
	got, err := c.BoardState(ctx, is)
	if err != nil {
		return err
	}
	if got == s {
		return nil
	}
	return c.SetBoardState(ctx, is, s)
}

func syncOne(ctx context.Context, c *Client, rep *Reporter, r Requirement, list []Issue) error {
	var open []Issue
	var last *Issue
	for i := range list {
		if list[i].State == "open" {
			open = append(open, list[i])
		}
		last = &list[i]
	}
	if len(open) > 1 {
		for _, is := range open[1:] {
			if err := rep.Report(ctx, is.Number, fmt.Sprintf("Issue #%d is also open for %s. The sync step changes only #%d.", open[0].Number, IssueTitle(r), open[0].Number)); err != nil {
				return err
			}
		}
	}
	if len(open) == 0 {
		if last != nil && last.Reason != "not_planned" {
			m, _ := ParseMarker(last.Body)
			if m.Hash != r.Hash {
				return rep.Report(ctx, last.Number, fmt.Sprintf("The text of %s changed after it was done. The sync step opens no new issue. Write a new requirement for the change.", r.ID))
			}
			return syncBoard(ctx, c, *last)
		}
		is, err := c.CreateIssue(ctx, IssueTitle(r), IssueBody(r, c.Repo), []string{Proposed})
		if err != nil {
			return err
		}
		return c.SetBoardState(ctx, is, Proposed)
	}

	is := open[0]
	m, _ := ParseMarker(is.Body)
	old := r
	old.Hash = m.Hash
	edited := is.Body != IssueBody(old, c.Repo)
	if edited {
		if err := rep.Report(ctx, is.Number, "Someone changed the body of this issue. The sync step does not overwrite it. Put the body back as the sync step wrote it."); err != nil {
			return err
		}
	}
	state, ok := StateOf(is)
	if !ok {
		return rep.Report(ctx, is.Number, fmt.Sprintf("This issue must have exactly one state label, but it has %d. The sync step does not change it, and the build agent does not build it.", len(StatesOf(is))))
	}
	if m.Hash != r.Hash {
		reason := fmt.Sprintf("The text of %s changed in the plan file.", r.ID)
		switch state {
		case Ready, Blocked:
			if err := c.SetState(ctx, &is, Proposed, reason+" The operator must read it again and set ready."); err != nil {
				return err
			}
		case Building, NeedsOperator:
			if err := c.SetState(ctx, &is, Blocked, reason+" The open pull request does not build the new text."); err != nil {
				return err
			}
		}
		if !edited {
			if err := c.UpdateIssueBody(ctx, is.Number, IssueBody(r, c.Repo)); err != nil {
				return err
			}
		}
		return nil
	}
	return syncBoard(ctx, c, is)
}

func syncGone(ctx context.Context, c *Client, rep *Reporter, is Issue, m Marker) error {
	state, ok := StateOf(is)
	if !ok {
		return rep.Report(ctx, is.Number, fmt.Sprintf("%s left its plan file, but this issue has %d state labels. The sync step does not change it.", m.ID, len(StatesOf(is))))
	}
	reason := fmt.Sprintf("%s is no longer in %s.", m.ID, m.Plan)
	switch state {
	case Building, NeedsOperator:
		return c.SetState(ctx, &is, Blocked, reason+" A pull request for it is still open. The operator decides what happens to it.")
	default:
		return c.Finish(ctx, &is, Dropped, reason)
	}
}
