// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Client makes the GitHub calls of the workflow tool. It uses REST for
// issues and pull requests, and GraphQL only for the project board.
// In dry-run mode, each write becomes one JSON line on Out and sends nothing.
type Client struct {
	BaseURL    string // for example https://api.github.com
	GraphQLURL string // for example https://api.github.com/graphql
	Token      string // read from the environment, never printed
	Repo       string // owner/name
	ProjectID  string // node ID of the project board; empty means no board
	DryRun     bool
	Out        io.Writer
	HTTP       *http.Client

	stateField   string
	stateOptions map[string]string
}

// Issue is the part of a GitHub issue that the tool uses.
type Issue struct {
	Number int
	NodeID string
	Title  string
	Body   string
	State  string // open or closed
	Reason string // completed or not_planned, for a closed issue
	Author string
	Labels []string
}

// HasLabel tells if the issue carries the label.
func (is Issue) HasLabel(name string) bool {
	return slices.Contains(is.Labels, name)
}

type apiUser struct {
	Login string `json:"login"`
}

type apiIssue struct {
	Number int     `json:"number"`
	NodeID string  `json:"node_id"`
	Title  string  `json:"title"`
	Body   string  `json:"body"`
	State  string  `json:"state"`
	Reason string  `json:"state_reason"`
	User   apiUser `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	PullRequest *struct{} `json:"pull_request"`
}

func (a apiIssue) issue() Issue {
	is := Issue{Number: a.Number, NodeID: a.NodeID, Title: a.Title, Body: a.Body, State: a.State, Reason: a.Reason, Author: a.User.Login}
	for _, l := range a.Labels {
		is.Labels = append(is.Labels, l.Name)
	}
	return is
}

// Pull is the part of a GitHub pull request that the tool uses.
type Pull struct {
	Number  int
	Title   string
	Body    string
	State   string
	Merged  bool
	Author  string
	HeadSHA string
	HeadRef string
	BaseRef string
}

type apiPull struct {
	Number   int     `json:"number"`
	Title    string  `json:"title"`
	Body     string  `json:"body"`
	State    string  `json:"state"`
	Merged   bool    `json:"merged"`
	MergedAt *string `json:"merged_at"`
	User     apiUser `json:"user"`
	Head     struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (a apiPull) pull() Pull {
	return Pull{Number: a.Number, Title: a.Title, Body: a.Body, State: a.State,
		Merged: a.Merged || a.MergedAt != nil, Author: a.User.Login,
		HeadSHA: a.Head.SHA, HeadRef: a.Head.Ref, BaseRef: a.Base.Ref}
}

// Event is one entry of an issue timeline.
type Event struct {
	Event     string
	Label     string
	Actor     string
	CreatedAt time.Time
}

// CheckRun is one job result on a commit.
type CheckRun struct {
	Name       string
	Status     string
	Conclusion string
}

// Review is one review of a pull request.
type Review struct {
	Author string
	State  string // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED
}

// Comment is one issue comment.
type Comment struct {
	Author string
	Body   string
}

func (c *Client) redact(s string) string {
	if c.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, c.Token, "[token]")
}

func (c *Client) do(ctx context.Context, method, rawURL string, in, out any) (http.Header, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, fmt.Errorf("%s", c.redact(err.Error()))
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github %s %s: %s", method, c.redact(rawURL), c.redact(err.Error()))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("github %s %s: status %d: %s", method, c.redact(rawURL), resp.StatusCode, c.redact(msg))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return nil, fmt.Errorf("github %s %s: %v", method, c.redact(rawURL), err)
		}
	}
	return resp.Header, nil
}

func (c *Client) repoURL(format string, args ...any) string {
	return strings.TrimRight(c.BaseURL, "/") + "/repos/" + c.Repo + fmt.Sprintf(format, args...)
}

// write sends a change, or prints it as one JSON line in dry-run mode.
func (c *Client) write(ctx context.Context, op map[string]any, method, rawURL string, in, out any) error {
	if c.DryRun {
		return c.dryRunLine(op)
	}
	_, err := c.do(ctx, method, rawURL, in, out)
	return err
}

// dryRunLine prints one planned change as a JSON line on Out.
func (c *Client) dryRunLine(op map[string]any) error {
	b, err := json.Marshal(op)
	if err != nil {
		return err
	}
	w := c.Out
	if w == nil {
		w = io.Discard
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

// getAll reads every page of a list endpoint.
func getAll[T any](ctx context.Context, c *Client, rawURL string) ([]T, error) {
	var all []T
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	for page := 1; ; page++ {
		var items []T
		if _, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s%sper_page=100&page=%d", rawURL, sep, page), nil, &items); err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) < 100 {
			return all, nil
		}
	}
}

// ListIssues returns every issue of the repository, open and closed,
// without pull requests.
func (c *Client) ListIssues(ctx context.Context) ([]Issue, error) {
	items, err := getAll[apiIssue](ctx, c, c.repoURL("/issues?state=all"))
	if err != nil {
		return nil, err
	}
	var out []Issue
	for _, a := range items {
		if a.PullRequest == nil {
			out = append(out, a.issue())
		}
	}
	return out, nil
}

// GetIssue returns one issue.
func (c *Client) GetIssue(ctx context.Context, n int) (Issue, error) {
	var a apiIssue
	_, err := c.do(ctx, http.MethodGet, c.repoURL("/issues/%d", n), nil, &a)
	return a.issue(), err
}

// CreateIssue opens an issue. In dry-run mode, the returned issue has number 0.
func (c *Client) CreateIssue(ctx context.Context, title, body string, labels []string) (Issue, error) {
	in := map[string]any{"title": title, "body": body, "labels": labels}
	var a apiIssue
	op := map[string]any{"op": "create_issue", "title": title, "labels": labels}
	if err := c.write(ctx, op, http.MethodPost, c.repoURL("/issues"), in, &a); err != nil {
		return Issue{}, err
	}
	if c.DryRun {
		return Issue{Title: title, Body: body, State: "open", Labels: labels}, nil
	}
	return a.issue(), nil
}

// UpdateIssueBody replaces the body of an issue.
func (c *Client) UpdateIssueBody(ctx context.Context, n int, body string) error {
	op := map[string]any{"op": "update_body", "issue": n}
	return c.write(ctx, op, http.MethodPatch, c.repoURL("/issues/%d", n), map[string]any{"body": body}, nil)
}

// AddLabels adds labels to an issue.
func (c *Client) AddLabels(ctx context.Context, n int, labels ...string) error {
	op := map[string]any{"op": "add_labels", "issue": n, "labels": labels}
	return c.write(ctx, op, http.MethodPost, c.repoURL("/issues/%d/labels", n), map[string]any{"labels": labels}, nil)
}

// RemoveLabel removes one label from an issue.
func (c *Client) RemoveLabel(ctx context.Context, n int, label string) error {
	op := map[string]any{"op": "remove_label", "issue": n, "label": label}
	return c.write(ctx, op, http.MethodDelete, c.repoURL("/issues/%d/labels/%s", n, url.PathEscape(label)), nil, nil)
}

// CloseIssue closes an issue with the reason completed or not_planned.
func (c *Client) CloseIssue(ctx context.Context, n int, reason string) error {
	op := map[string]any{"op": "close_issue", "issue": n, "reason": reason}
	return c.write(ctx, op, http.MethodPatch, c.repoURL("/issues/%d", n), map[string]any{"state": "closed", "state_reason": reason}, nil)
}

// AddComment writes a comment on an issue or a pull request.
func (c *Client) AddComment(ctx context.Context, n int, body string) error {
	op := map[string]any{"op": "comment", "issue": n, "body": body}
	return c.write(ctx, op, http.MethodPost, c.repoURL("/issues/%d/comments", n), map[string]any{"body": body}, nil)
}

// Comments returns the comments of an issue or a pull request.
func (c *Client) Comments(ctx context.Context, n int) ([]Comment, error) {
	type apiComment struct {
		Body string  `json:"body"`
		User apiUser `json:"user"`
	}
	items, err := getAll[apiComment](ctx, c, c.repoURL("/issues/%d/comments", n))
	if err != nil {
		return nil, err
	}
	var out []Comment
	for _, a := range items {
		out = append(out, Comment{Author: a.User.Login, Body: a.Body})
	}
	return out, nil
}

// Timeline returns the label events of an issue, oldest first.
func (c *Client) Timeline(ctx context.Context, n int) ([]Event, error) {
	type apiEvent struct {
		Event string `json:"event"`
		Label *struct {
			Name string `json:"name"`
		} `json:"label"`
		Actor *struct {
			Login string `json:"login"`
		} `json:"actor"`
		CreatedAt time.Time `json:"created_at"`
	}
	items, err := getAll[apiEvent](ctx, c, c.repoURL("/issues/%d/timeline", n))
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, a := range items {
		ev := Event{Event: a.Event, CreatedAt: a.CreatedAt}
		if a.Label != nil {
			ev.Label = a.Label.Name
		}
		if a.Actor != nil {
			ev.Actor = a.Actor.Login
		}
		out = append(out, ev)
	}
	return out, nil
}

// OpenPulls returns the open pull requests of the repository.
func (c *Client) OpenPulls(ctx context.Context) ([]Pull, error) {
	items, err := getAll[apiPull](ctx, c, c.repoURL("/pulls?state=open"))
	if err != nil {
		return nil, err
	}
	var out []Pull
	for _, a := range items {
		out = append(out, a.pull())
	}
	return out, nil
}

// GetPull returns one pull request.
func (c *Client) GetPull(ctx context.Context, n int) (Pull, error) {
	var a apiPull
	_, err := c.do(ctx, http.MethodGet, c.repoURL("/pulls/%d", n), nil, &a)
	return a.pull(), err
}

// PullFiles returns the paths that a pull request changes. A renamed file
// gives both its old and its new path.
func (c *Client) PullFiles(ctx context.Context, n int) ([]string, error) {
	type apiFile struct {
		Filename         string `json:"filename"`
		PreviousFilename string `json:"previous_filename"`
	}
	items, err := getAll[apiFile](ctx, c, c.repoURL("/pulls/%d/files", n))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range items {
		out = append(out, f.Filename)
		if f.PreviousFilename != "" {
			out = append(out, f.PreviousFilename)
		}
	}
	return out, nil
}

// Reviews returns the reviews of a pull request, oldest first.
func (c *Client) Reviews(ctx context.Context, n int) ([]Review, error) {
	type apiReview struct {
		State string  `json:"state"`
		User  apiUser `json:"user"`
	}
	items, err := getAll[apiReview](ctx, c, c.repoURL("/pulls/%d/reviews", n))
	if err != nil {
		return nil, err
	}
	var out []Review
	for _, a := range items {
		out = append(out, Review{Author: a.User.Login, State: a.State})
	}
	return out, nil
}

// CheckRuns returns the job results on a commit.
func (c *Client) CheckRuns(ctx context.Context, sha string) ([]CheckRun, error) {
	var out []CheckRun
	for page := 1; ; page++ {
		var resp struct {
			CheckRuns []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			} `json:"check_runs"`
		}
		if _, err := c.do(ctx, http.MethodGet, c.repoURL("/commits/%s/check-runs?per_page=100&page=%d", url.PathEscape(sha), page), nil, &resp); err != nil {
			return nil, err
		}
		for _, r := range resp.CheckRuns {
			out = append(out, CheckRun{Name: r.Name, Status: r.Status, Conclusion: r.Conclusion})
		}
		if len(resp.CheckRuns) < 100 {
			return out, nil
		}
	}
}

// MergePull merges a pull request at the given head commit.
func (c *Client) MergePull(ctx context.Context, n int, sha string) error {
	op := map[string]any{"op": "merge", "pull": n, "sha": sha}
	return c.write(ctx, op, http.MethodPut, c.repoURL("/pulls/%d/merge", n), map[string]any{"sha": sha, "merge_method": "squash"}, nil)
}

// ClosePull closes a pull request without a merge.
func (c *Client) ClosePull(ctx context.Context, n int) error {
	op := map[string]any{"op": "close_pull", "pull": n}
	return c.write(ctx, op, http.MethodPatch, c.repoURL("/pulls/%d", n), map[string]any{"state": "closed"}, nil)
}

func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if _, err := c.do(ctx, http.MethodPost, c.GraphQLURL, map[string]any{"query": query, "variables": vars}, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return fmt.Errorf("github graphql: %s", c.redact(resp.Errors[0].Message))
	}
	if out != nil {
		return json.Unmarshal(resp.Data, out)
	}
	return nil
}

func (c *Client) loadStateField(ctx context.Context) error {
	if c.stateOptions != nil {
		return nil
	}
	var data struct {
		Node struct {
			Field struct {
				ID      string `json:"id"`
				Options []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"options"`
			} `json:"field"`
		} `json:"node"`
	}
	q := `query($p: ID!) { node(id: $p) { ... on ProjectV2 { field(name: "State") { ... on ProjectV2SingleSelectField { id options { id name } } } } } }`
	if err := c.graphql(ctx, q, map[string]any{"p": c.ProjectID}, &data); err != nil {
		return err
	}
	if data.Node.Field.ID == "" {
		return fmt.Errorf("the project board has no single select field State")
	}
	c.stateField = data.Node.Field.ID
	c.stateOptions = map[string]string{}
	for _, o := range data.Node.Field.Options {
		c.stateOptions[o.Name] = o.ID
	}
	return nil
}

// SetBoardState puts the issue on the project board and sets its State
// field. Without a ProjectID, it does nothing.
func (c *Client) SetBoardState(ctx context.Context, is Issue, state string) error {
	if c.ProjectID == "" {
		return nil
	}
	if c.DryRun {
		return c.dryRunLine(map[string]any{"op": "board_state", "issue": is.Number, "state": state})
	}
	if err := c.loadStateField(ctx); err != nil {
		return err
	}
	opt, ok := c.stateOptions[state]
	if !ok {
		return fmt.Errorf("the State field of the project board has no option %s", state)
	}
	var added struct {
		Add struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		} `json:"addProjectV2ItemById"`
	}
	addQ := `mutation($p: ID!, $c: ID!) { addProjectV2ItemById(input: {projectId: $p, contentId: $c}) { item { id } } }`
	if err := c.graphql(ctx, addQ, map[string]any{"p": c.ProjectID, "c": is.NodeID}, &added); err != nil {
		return err
	}
	setQ := `mutation($p: ID!, $i: ID!, $f: ID!, $o: String!) { updateProjectV2ItemFieldValue(input: {projectId: $p, itemId: $i, fieldId: $f, value: {singleSelectOptionId: $o}}) { projectV2Item { id } } }`
	return c.graphql(ctx, setQ, map[string]any{"p": c.ProjectID, "i": added.Add.Item.ID, "f": c.stateField, "o": opt}, nil)
}

// BoardState returns the State field of the issue on the project board.
// It returns an empty string if the issue is not on the board or has no
// State. Without a ProjectID, it returns an empty string.
func (c *Client) BoardState(ctx context.Context, is Issue) (string, error) {
	if c.ProjectID == "" || is.NodeID == "" {
		return "", nil
	}
	var data struct {
		Node struct {
			ProjectItems struct {
				Nodes []struct {
					Project struct {
						ID string `json:"id"`
					} `json:"project"`
					Field *struct {
						Name string `json:"name"`
					} `json:"fieldValueByName"`
				} `json:"nodes"`
			} `json:"projectItems"`
		} `json:"node"`
	}
	q := `query($i: ID!) { node(id: $i) { ... on Issue { projectItems(first: 20) { nodes { project { id } fieldValueByName(name: "State") { ... on ProjectV2ItemFieldSingleSelectValue { name } } } } } } }`
	if err := c.graphql(ctx, q, map[string]any{"i": is.NodeID}, &data); err != nil {
		return "", err
	}
	for _, n := range data.Node.ProjectItems.Nodes {
		if n.Project.ID == c.ProjectID && n.Field != nil {
			return n.Field.Name, nil
		}
	}
	return "", nil
}
