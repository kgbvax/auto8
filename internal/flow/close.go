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

// Close finishes the issues of pull request n after it closed. If it
// merged, each issue with a summary block gets the block as a comment and
// closes as done. An issue of the build without a block also closes, with
// a comment that names the gap. If the pull request closed without a
// merge, the building issues of its build go to blocked.
func Close(ctx context.Context, c *Client, app string, n int) error {
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
	build, hasBuild := ParseBuildMarker(pr.Body)
	if pr.Author != app {
		hasBuild = false
	}

	if !pr.Merged {
		if !hasBuild {
			return nil
		}
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

	done := map[string]bool{}
	for _, s := range ParseSummaries(pr.Body) {
		is, ok := openIssue(s.Plan, s.ID)
		if !ok {
			continue
		}
		text := fmt.Sprintf("Done in pull request #%d.\n\n%s", n, s.Text)
		if err := c.Finish(ctx, &is, Done, text); err != nil {
			return err
		}
		done[key(s.Plan, s.ID)] = true
	}
	if hasBuild {
		for _, id := range build.IDs {
			if done[key(build.Plan, id)] {
				continue
			}
			is, ok := openIssue(build.Plan, id)
			if !ok {
				continue
			}
			text := fmt.Sprintf("Pull request #%d merged, but it has no summary block for %s. The feature summary and the implementation summary are missing.", n, id)
			if err := c.Finish(ctx, &is, Done, text); err != nil {
				return err
			}
		}
	}
	return nil
}
