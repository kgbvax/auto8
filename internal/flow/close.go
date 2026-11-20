// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Summary is one summary block of a pull request body. The build agent
// writes one block for each requirement, with a feature summary and an
// implementation summary.
type Summary struct {
	Plan string
	ID   string
	Text string
}

var summaryRe = regexp.MustCompile(`(?s)<!-- auto8-summary plan=(\S+) id=(R[0-9]+) -->(.*?)<!-- /auto8-summary -->`)

// SummaryBlock returns the text of one summary block.
func SummaryBlock(plan, id, text string) string {
	return fmt.Sprintf("<!-- auto8-summary plan=%s id=%s -->\n%s\n<!-- /auto8-summary -->", plan, id, strings.TrimSpace(text))
}

// ParseSummaries returns the summary blocks of a pull request body.
func ParseSummaries(body string) []Summary {
	var out []Summary
	for _, m := range summaryRe.FindAllStringSubmatch(body, -1) {
		out = append(out, Summary{Plan: m[1], ID: m[2], Text: strings.TrimSpace(m[3])})
	}
	return out
}

// Close finishes the issues of pull request n after it closed.
//
// A merged pull request of app closes the issues of its build as done. An
// issue with a summary block gets the block as a comment. An issue
// without one gets a comment that names the gap. A summary block outside
// the build, or for an issue that is not building or needs-operator,
// changes nothing. A merged pull request of operator closes each issue
// that has a summary block, so the operator can repair a missed close.
// Summary blocks of other authors change nothing. If a pull request of
// app closed without a merge, the issues of its build go to blocked.
func Close(ctx context.Context, c *Client, app, operator string, n int) error {
	pr, err := c.GetPull(ctx, n)
	if err != nil {
		return err
	}
	if pr.State != "closed" {
		return fmt.Errorf("pull request #%d is still open", n)
	}
	all, err := c.ListIssues(ctx)
	if err != nil {
		return err
	}
	managed := managedIssues(all, app)
	openIssue := func(plan, id string) (Issue, bool) {
		return firstOpen(managed[key(plan, id)])
	}

	if pr.Author == operator {
		if !pr.Merged {
			return nil
		}
		for _, s := range ParseSummaries(pr.Body) {
			is, ok := openIssue(s.Plan, s.ID)
			if !ok {
				continue
			}
			if err := c.Finish(ctx, &is, Done, fmt.Sprintf("Done in pull request #%d.\n\n%s", n, s.Text)); err != nil {
				return err
			}
		}
		return nil
	}
	if pr.Author != app {
		return nil
	}
	build, ok := ParseBuildMarker(pr.Body)
	if !ok {
		return nil
	}

	if !pr.Merged {
		for _, id := range build.IDs {
			is, ok := openIssue(build.Plan, id)
			if !ok {
				continue
			}
			if s, ok := StateOf(is); ok && s == Blocked {
				continue
			}
			if err := c.SetState(ctx, &is, Blocked, fmt.Sprintf("Pull request #%d closed without a merge. Set ready to build it again.", n)); err != nil {
				return err
			}
		}
		return nil
	}

	summaries := map[string]Summary{}
	for _, s := range ParseSummaries(pr.Body) {
		if s.Plan == build.Plan {
			summaries[s.ID] = s
		}
	}
	for _, id := range build.IDs {
		is, ok := openIssue(build.Plan, id)
		if !ok {
			continue
		}
		if s, ok := StateOf(is); !ok || (s != Building && s != NeedsOperator) {
			continue
		}
		text := fmt.Sprintf("Pull request #%d merged, but it has no summary block for %s. The feature summary and the implementation summary are missing.", n, id)
		if s, ok := summaries[id]; ok {
			text = fmt.Sprintf("Done in pull request #%d.\n\n%s", n, s.Text)
		}
		if err := c.Finish(ctx, &is, Done, text); err != nil {
			return err
		}
	}
	return nil
}
