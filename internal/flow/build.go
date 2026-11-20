// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// BuildMarker is what the build step writes into the body of each pull
// request it opens. It names the plan and the requirements of the build.
type BuildMarker struct {
	Plan string
	IDs  []string
}

var buildMarkerRe = regexp.MustCompile(`<!-- auto8-build plan=(\S+) ids=(R[0-9]+(?:,R[0-9]+)*) -->`)

// BuildMarkerText returns the HTML comment for a build.
func BuildMarkerText(plan string, ids []string) string {
	return fmt.Sprintf("<!-- auto8-build plan=%s ids=%s -->", plan, strings.Join(ids, ","))
}

// ParseBuildMarker finds the build marker in a pull request body.
func ParseBuildMarker(body string) (BuildMarker, bool) {
	m := buildMarkerRe.FindStringSubmatch(body)
	if m == nil {
		return BuildMarker{}, false
	}
	return BuildMarker{Plan: m[1], IDs: strings.Split(m[2], ",")}, true
}

// AppPull is an open pull request of the App with its build marker.
type AppPull struct {
	Pull
	Build BuildMarker
}

// openAppPulls returns the open pull requests of app that carry a build marker.
func openAppPulls(ctx context.Context, c *Client, app string) ([]AppPull, error) {
	pulls, err := c.OpenPulls(ctx)
	if err != nil {
		return nil, err
	}
	var out []AppPull
	for _, p := range pulls {
		if p.Author != app {
			continue
		}
		if b, ok := ParseBuildMarker(p.Body); ok {
			out = append(out, AppPull{Pull: p, Build: b})
		}
	}
	return out, nil
}

// openManaged returns the open issues of app with a marker, by requirement.
func openManaged(ctx context.Context, c *Client, app string) (map[string]Issue, error) {
	all, err := c.ListIssues(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]Issue{}
	for k, list := range managedIssues(all, app) {
		if is, ok := firstOpen(list); ok {
			out[k] = is
		}
	}
	return out, nil
}

// Reap sets blocked on each building issue that no open pull request of
// app covers. It returns the numbers of the issues that it changed.
func Reap(ctx context.Context, c *Client, app string) ([]int, error) {
	issues, err := openManaged(ctx, c, app)
	if err != nil {
		return nil, err
	}
	pulls, err := openAppPulls(ctx, c, app)
	if err != nil {
		return nil, err
	}
	covered := map[string]bool{}
	for _, p := range pulls {
		for _, id := range p.Build.IDs {
			covered[key(p.Build.Plan, id)] = true
		}
	}
	var keys []string
	for k := range issues {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var changed []int
	for _, k := range keys {
		is := issues[k]
		if s, ok := StateOf(is); !ok || s != Building || covered[k] {
			continue
		}
		if err := c.SetState(ctx, &is, Blocked, "The build stopped without a pull request. Set ready to build it again."); err != nil {
			return changed, err
		}
		changed = append(changed, is.Number)
	}
	return changed, nil
}

// Pick is the result of the pick step.
type Pick struct {
	Plan   string   `json:"plan"`
	IDs    []string `json:"ids"`
	Issues []int    `json:"issues"`
}

// readySince returns the time at which operator set the label ready for
// the last time. It returns ok false if the last ready label came from
// someone else.
func readySince(ctx context.Context, c *Client, n int, operator string) (time.Time, bool, error) {
	events, err := c.Timeline(ctx, n)
	if err != nil {
		return time.Time{}, false, err
	}
	var last *Event
	for i := range events {
		if events[i].Event == "labeled" && events[i].Label == Ready {
			last = &events[i]
		}
	}
	if last == nil || last.Actor != operator {
		return time.Time{}, false, nil
	}
	return last.CreatedAt, true, nil
}

// PickNext takes the plan of the oldest valid ready issue and sets
// building on all valid ready issues of that plan. A valid issue has
// exactly one state label, its last ready label came from operator, its
// marker hash matches the plan on main, and its plan has no open pull
// request of app. Without a valid issue, PickNext returns nil.
func PickNext(ctx context.Context, c *Client, plans []Plan, app, operator string) (*Pick, error) {
	current := map[string]string{}
	for _, p := range plans {
		for _, r := range p.Requirements {
			current[key(r.Plan, r.ID)] = r.Hash
		}
	}
	issues, err := openManaged(ctx, c, app)
	if err != nil {
		return nil, err
	}
	pulls, err := openAppPulls(ctx, c, app)
	if err != nil {
		return nil, err
	}
	busy := map[string]bool{}
	for _, p := range pulls {
		busy[p.Build.Plan] = true
	}

	type cand struct {
		is    Issue
		m     Marker
		since time.Time
	}
	byPlan := map[string][]cand{}
	var oldest *cand
	for k, is := range issues {
		if s, ok := StateOf(is); !ok || s != Ready {
			continue
		}
		m, _ := ParseMarker(is.Body)
		if busy[m.Plan] || current[k] != m.Hash {
			continue
		}
		since, ok, err := readySince(ctx, c, is.Number, operator)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		cd := cand{is: is, m: m, since: since}
		byPlan[m.Plan] = append(byPlan[m.Plan], cd)
		if oldest == nil || since.Before(oldest.since) || (since.Equal(oldest.since) && is.Number < oldest.is.Number) {
			oldest = &cd
		}
	}
	if oldest == nil {
		return nil, nil
	}
	list := byPlan[oldest.m.Plan]
	sort.Slice(list, func(i, j int) bool { return list[i].is.Number < list[j].is.Number })
	pick := &Pick{Plan: oldest.m.Plan}
	for _, cd := range list {
		is := cd.is
		if err := c.SetState(ctx, &is, Building, ""); err != nil {
			return pick, err
		}
		pick.IDs = append(pick.IDs, cd.m.ID)
		pick.Issues = append(pick.Issues, is.Number)
	}
	return pick, nil
}

// Mark sets state on the issues of plan with the given IDs. It is the only
// way for the build agent to change a state. It accepts only blocked and
// needs-operator, and only for issues that are building or need the
// operator. Setting blocked also closes the open pull requests of app for
// that plan, so the plan is free for a new build.
func Mark(ctx context.Context, c *Client, app, plan string, ids []string, state, reason string) error {
	if state != Blocked && state != NeedsOperator {
		return fmt.Errorf("mark accepts only %s and %s, not %q", Blocked, NeedsOperator, state)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("mark needs a reason")
	}
	issues, err := openManaged(ctx, c, app)
	if err != nil {
		return err
	}
	var targets []*Issue
	for _, id := range ids {
		is, ok := issues[key(plan, id)]
		if !ok {
			return fmt.Errorf("no open issue for %s %s", plan, id)
		}
		s, ok := StateOf(is)
		if !ok || (s != Building && s != NeedsOperator) {
			return fmt.Errorf("issue #%d of %s is not building, so mark does not change it", is.Number, id)
		}
		targets = append(targets, &is)
	}
	if state == Blocked {
		return BlockAndRelease(ctx, c, app, plan, targets, reason)
	}
	for _, is := range targets {
		if err := c.SetState(ctx, is, state, reason); err != nil {
			return err
		}
	}
	return nil
}

// BlockAndRelease sets blocked on the issues and closes each open pull
// request of app for plan with a comment that names the reason.
func BlockAndRelease(ctx context.Context, c *Client, app, plan string, issues []*Issue, reason string) error {
	for _, is := range issues {
		if s, ok := StateOf(*is); ok && s == Blocked {
			continue
		}
		if err := c.SetState(ctx, is, Blocked, reason); err != nil {
			return err
		}
	}
	pulls, err := openAppPulls(ctx, c, app)
	if err != nil {
		return err
	}
	for _, p := range pulls {
		if p.Build.Plan != plan {
			continue
		}
		if err := c.AddComment(ctx, p.Number, "The issues of this pull request are blocked: "+reason+" The operator sets ready to build the plan again."); err != nil {
			return err
		}
		if err := c.ClosePull(ctx, p.Number); err != nil {
			return err
		}
	}
	return nil
}
