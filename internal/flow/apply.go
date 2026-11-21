// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Decision is one state decision of the build agent. The agent writes its
// decisions to a file, because its own token can only read. A later step
// without Claude applies them with ApplyDecisions.
type Decision struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// ParseDecisions reads the decisions file of the build agent. An empty
// file means no decisions. The file is a JSON array of decisions.
func ParseDecisions(data []byte) ([]Decision, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var ds []Decision
	if err := dec.Decode(&ds); err != nil {
		return nil, fmt.Errorf("the decisions file is not a JSON array of decisions: %w", err)
	}
	return ds, nil
}

// CheckDecisions refuses the whole list if one decision is wrong. A
// decision must name a picked requirement, a state of blocked or
// needs-operator, and a reason. Each requirement can have one decision.
func CheckDecisions(pick Pick, ds []Decision) error {
	picked := map[string]bool{}
	for _, id := range pick.IDs {
		picked[id] = true
	}
	seen := map[string]bool{}
	for _, d := range ds {
		if !picked[d.ID] {
			return fmt.Errorf("decision for %q refused: this build did not pick it", d.ID)
		}
		if d.State != Blocked && d.State != NeedsOperator {
			return fmt.Errorf("decision for %s refused: state %q is not %s or %s", d.ID, d.State, Blocked, NeedsOperator)
		}
		if strings.TrimSpace(d.Reason) == "" {
			return fmt.Errorf("decision for %s refused: it has no reason", d.ID)
		}
		if seen[d.ID] {
			return fmt.Errorf("decision for %s refused: it has more than one decision", d.ID)
		}
		seen[d.ID] = true
	}
	return nil
}

// ApplyDecisions applies the decisions with the given state, after
// CheckDecisions accepts the whole list. It returns the pick without the
// requirements that are now blocked.
//
// The build step applies blocked before it opens the pull request,
// because Mark with blocked closes the open App pull requests of the plan.
// It applies needs-operator after the pull request opens.
func ApplyDecisions(ctx context.Context, c *Client, app string, pick Pick, ds []Decision, state string) (Pick, error) {
	if err := CheckDecisions(pick, ds); err != nil {
		return pick, err
	}
	if state != Blocked && state != NeedsOperator {
		return pick, fmt.Errorf("apply accepts only %s and %s, not %q", Blocked, NeedsOperator, state)
	}
	gone := map[string]bool{}
	for _, d := range ds {
		if d.State != state {
			continue
		}
		if err := Mark(ctx, c, app, pick.Plan, []string{d.ID}, d.State, d.Reason); err != nil {
			return pick, err
		}
		if d.State == Blocked {
			gone[d.ID] = true
		}
	}
	rest := Pick{Plan: pick.Plan}
	for i, id := range pick.IDs {
		if gone[id] {
			continue
		}
		rest.IDs = append(rest.IDs, id)
		if i < len(pick.Issues) {
			rest.Issues = append(rest.Issues, pick.Issues[i])
		}
	}
	return rest, nil
}

var (
	anyBuildMarkerRe = regexp.MustCompile(`<!--\s*auto8-build\b[^>]*-->\n?`)
	anySummaryRe     = regexp.MustCompile(`(?s)<!--\s*auto8-summary plan=(\S+) id=(R[0-9]+)\s*-->.*?<!--\s*/auto8-summary\s*-->\n?`)
	// closingRe finds the GitHub closing keywords, for example "Fixes #3"
	// or "closes owner/repo#3".
	closingRe = regexp.MustCompile(`(?i)\b(close[sd]?|fix(?:e[sd])?|resolve[sd]?)(:?\s+)(?:[\w.-]+/[\w.-]+)?#([0-9]+)`)
)

// PullBody builds the body of the pull request of a build from the text
// of the agent. The build marker comes from the pick, never from the
// agent. The body keeps only the summary blocks of the picked
// requirements. It names the issues without a closing keyword, because
// the close step closes them after it copied the summaries.
func PullBody(pick Pick, agent string) string {
	picked := map[string]bool{}
	for _, id := range pick.IDs {
		picked[id] = true
	}
	agent = anyBuildMarkerRe.ReplaceAllString(agent, "")
	agent = anySummaryRe.ReplaceAllStringFunc(agent, func(s string) string {
		m := anySummaryRe.FindStringSubmatch(s)
		if m[1] == pick.Plan && picked[m[2]] {
			return s
		}
		return ""
	})
	agent = closingRe.ReplaceAllString(agent, "${1}${2}issue ${3}")
	var b strings.Builder
	b.WriteString(BuildMarkerText(pick.Plan, pick.IDs))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "Build of `%s`:", pick.Plan)
	for i, id := range pick.IDs {
		if i < len(pick.Issues) {
			fmt.Fprintf(&b, " %s (#%d)", id, pick.Issues[i])
		} else {
			fmt.Fprintf(&b, " %s", id)
		}
		if i < len(pick.IDs)-1 {
			b.WriteString(",")
		}
	}
	b.WriteString(".\n\n")
	b.WriteString(strings.TrimSpace(agent))
	b.WriteString("\n")
	return b.String()
}
