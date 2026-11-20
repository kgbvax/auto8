// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	fakeToken = "ghs_faketoken1234567890"
	appLogin  = "opsotter-auto8[bot]"
	operator  = "kgbvax"
)

type fakeIssue struct {
	Number     int
	Title      string
	Body       string
	State      string
	Reason     string
	Author     string
	Labels     []string
	Comments   []Comment
	Events     []Event
	BoardState string
}

type fakePull struct {
	Number  int
	Title   string
	Body    string
	State   string
	Merged  bool
	Author  string
	HeadSHA string
	Files   []string
	Reviews []Review
}

// fakeGitHub is an in-memory GitHub for the tests. It answers the REST and
// GraphQL calls of Client and records every write.
type fakeGitHub struct {
	t      *testing.T
	mu     sync.Mutex
	srv    *httptest.Server
	actor  string // login of the token holder
	next   int
	issues map[int]*fakeIssue
	pulls  map[int]*fakePull
	checks map[string][]CheckRun
	writes []string
	clock  time.Time
	failOn string // a path part; a request that contains it gets status 500
}

func newFake(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{t: t, actor: appLogin, next: 1, issues: map[int]*fakeIssue{}, pulls: map[int]*fakePull{},
		checks: map[string][]CheckRun{}, clock: time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) client() *Client {
	return &Client{BaseURL: f.srv.URL, GraphQLURL: f.srv.URL + "/graphql", Token: fakeToken, Repo: "opsotter/auto8", ProjectID: "PVT_1"}
}

func (f *fakeGitHub) tick() time.Time {
	f.clock = f.clock.Add(time.Minute)
	return f.clock
}

// addIssue puts an issue into the fake. The label events name author as actor.
func (f *fakeGitHub) addIssue(author, title, body string, labels ...string) *fakeIssue {
	f.mu.Lock()
	defer f.mu.Unlock()
	is := &fakeIssue{Number: f.next, Title: title, Body: body, State: "open", Author: author}
	f.next++
	for _, l := range labels {
		is.Labels = append(is.Labels, l)
		is.Events = append(is.Events, Event{Event: "labeled", Label: l, Actor: author, CreatedAt: f.tick()})
	}
	f.issues[is.Number] = is
	return is
}

// label adds a label to an issue as actor, as a person in the web page would.
func (f *fakeGitHub) label(n int, actor, l string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	is := f.issues[n]
	is.Labels = append(is.Labels, l)
	is.Events = append(is.Events, Event{Event: "labeled", Label: l, Actor: actor, CreatedAt: f.tick()})
}

func (f *fakeGitHub) addPull(author, body, sha string, files ...string) *fakePull {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := &fakePull{Number: 1000 + f.next, Body: body, State: "open", Author: author, HeadSHA: sha, Files: files}
	f.next++
	f.pulls[p.Number] = p
	return p
}

func (f *fakeGitHub) issue(n int) *fakeIssue {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.issues[n]
}

func (f *fakeGitHub) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}

func (f *fakeGitHub) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		f.t.Errorf("fake: %v", err)
	}
}

func issueJSON(is *fakeIssue) map[string]any {
	var labels []map[string]string
	for _, l := range is.Labels {
		labels = append(labels, map[string]string{"name": l})
	}
	return map[string]any{"number": is.Number, "node_id": fmt.Sprintf("I_%d", is.Number), "title": is.Title, "body": is.Body,
		"state": is.State, "user": map[string]string{"login": is.Author}, "labels": labels}
}

func page[T any](r *http.Request, all []T) []T {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if p < 1 {
		p = 1
	}
	if per < 1 {
		per = 30
	}
	lo := (p - 1) * per
	if lo >= len(all) {
		return []T{}
	}
	hi := lo + per
	if hi > len(all) {
		hi = len(all)
	}
	return all[lo:hi]
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+fakeToken {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	if f.failOn != "" && strings.Contains(r.URL.Path, f.failOn) {
		http.Error(w, `{"message":"Server Error"}`, http.StatusInternalServerError)
		return
	}
	var in map[string]any
	if r.Body != nil {
		data, _ := io.ReadAll(r.Body)
		if len(data) > 0 {
			_ = json.Unmarshal(data, &in)
		}
	}
	if r.Method != http.MethodGet && r.URL.Path != "/graphql" {
		f.writes = append(f.writes, r.Method+" "+r.URL.Path)
	}
	if r.URL.Path == "/graphql" {
		f.graphql(w, in)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/repos/opsotter/auto8/"), "/")
	num := func(i int) int {
		if i >= len(parts) {
			return 0
		}
		n, _ := strconv.Atoi(parts[i])
		return n
	}
	notFound := func() { http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound) }

	switch {
	case parts[0] == "issues" && len(parts) == 1 && r.Method == http.MethodGet:
		var all []map[string]any
		for n := 1; n < f.next+1000; n++ {
			if is, ok := f.issues[n]; ok {
				all = append(all, issueJSON(is))
			}
		}
		f.json(w, page(r, all))
	case parts[0] == "issues" && len(parts) == 1 && r.Method == http.MethodPost:
		is := &fakeIssue{Number: f.next, Title: in["title"].(string), Body: in["body"].(string), State: "open", Author: f.actor}
		f.next++
		if ls, ok := in["labels"].([]any); ok {
			for _, l := range ls {
				is.Labels = append(is.Labels, l.(string))
				is.Events = append(is.Events, Event{Event: "labeled", Label: l.(string), Actor: f.actor, CreatedAt: f.tick()})
			}
		}
		f.issues[is.Number] = is
		f.json(w, issueJSON(is))
	case parts[0] == "issues" && len(parts) == 2:
		is, ok := f.issues[num(1)]
		if !ok {
			notFound()
			return
		}
		if r.Method == http.MethodPatch {
			if b, ok := in["body"].(string); ok {
				is.Body = b
			}
			if s, ok := in["state"].(string); ok {
				is.State = s
			}
			if s, ok := in["state_reason"].(string); ok {
				is.Reason = s
			}
		}
		f.json(w, issueJSON(is))
	case parts[0] == "issues" && len(parts) >= 3 && parts[2] == "labels":
		is, ok := f.issues[num(1)]
		if !ok {
			notFound()
			return
		}
		if r.Method == http.MethodPost {
			for _, l := range in["labels"].([]any) {
				if !(Issue{Labels: is.Labels}).HasLabel(l.(string)) {
					is.Labels = append(is.Labels, l.(string))
				}
				is.Events = append(is.Events, Event{Event: "labeled", Label: l.(string), Actor: f.actor, CreatedAt: f.tick()})
			}
		} else if r.Method == http.MethodDelete {
			name, _ := url.PathUnescape(parts[3])
			var keep []string
			found := false
			for _, l := range is.Labels {
				if l == name {
					found = true
					continue
				}
				keep = append(keep, l)
			}
			if !found {
				notFound()
				return
			}
			is.Labels = keep
			is.Events = append(is.Events, Event{Event: "unlabeled", Label: name, Actor: f.actor, CreatedAt: f.tick()})
		}
		f.json(w, []any{})
	case parts[0] == "issues" && len(parts) == 3 && parts[2] == "comments":
		n := num(1)
		var comments *[]Comment
		if is, ok := f.issues[n]; ok {
			comments = &is.Comments
		} else if p, ok := f.pulls[n]; ok {
			_ = p
			comments = new([]Comment)
		} else {
			notFound()
			return
		}
		if r.Method == http.MethodPost {
			*comments = append(*comments, Comment{Author: f.actor, Body: in["body"].(string)})
			f.json(w, map[string]any{})
			return
		}
		var out []map[string]any
		for _, c := range *comments {
			out = append(out, map[string]any{"body": c.Body, "user": map[string]string{"login": c.Author}})
		}
		f.json(w, page(r, out))
	case parts[0] == "issues" && len(parts) == 3 && parts[2] == "timeline":
		is, ok := f.issues[num(1)]
		if !ok {
			notFound()
			return
		}
		var out []map[string]any
		for _, e := range is.Events {
			out = append(out, map[string]any{"event": e.Event, "label": map[string]string{"name": e.Label},
				"actor": map[string]string{"login": e.Actor}, "created_at": e.CreatedAt})
		}
		f.json(w, page(r, out))
	case parts[0] == "pulls" && len(parts) == 1:
		var out []map[string]any
		for n := 1000; n < 1000+f.next; n++ {
			if p, ok := f.pulls[n]; ok && p.State == "open" {
				out = append(out, pullJSON(p))
			}
		}
		f.json(w, page(r, out))
	case parts[0] == "pulls" && len(parts) >= 2:
		p, ok := f.pulls[num(1)]
		if !ok {
			notFound()
			return
		}
		switch {
		case len(parts) == 2 && r.Method == http.MethodPatch:
			if s, ok := in["state"].(string); ok {
				p.State = s
			}
			f.json(w, pullJSON(p))
		case len(parts) == 2:
			f.json(w, pullJSON(p))
		case parts[2] == "files":
			var out []map[string]string
			for _, fn := range p.Files {
				out = append(out, map[string]string{"filename": fn})
			}
			f.json(w, page(r, out))
		case parts[2] == "reviews":
			var out []map[string]any
			for _, rv := range p.Reviews {
				out = append(out, map[string]any{"state": rv.State, "user": map[string]string{"login": rv.Author}})
			}
			f.json(w, page(r, out))
		case parts[2] == "merge":
			if in["sha"] != p.HeadSHA {
				http.Error(w, `{"message":"Head branch was modified"}`, http.StatusConflict)
				return
			}
			p.Merged, p.State = true, "closed"
			f.json(w, map[string]any{"merged": true})
		default:
			notFound()
		}
	case parts[0] == "commits" && len(parts) == 3 && parts[2] == "check-runs":
		var out []map[string]string
		for _, c := range f.checks[parts[1]] {
			out = append(out, map[string]string{"name": c.Name, "status": c.Status, "conclusion": c.Conclusion})
		}
		f.json(w, map[string]any{"check_runs": page(r, out)})
	default:
		notFound()
	}
}

func pullJSON(p *fakePull) map[string]any {
	return map[string]any{"number": p.Number, "title": p.Title, "body": p.Body, "state": p.State, "merged": p.Merged,
		"user": map[string]string{"login": p.Author}, "head": map[string]string{"sha": p.HeadSHA, "ref": "feat"},
		"base": map[string]string{"ref": "main"}}
}

func (f *fakeGitHub) graphql(w http.ResponseWriter, in map[string]any) {
	q, _ := in["query"].(string)
	vars, _ := in["variables"].(map[string]any)
	switch {
	case strings.Contains(q, "field(name:"):
		var opts []map[string]string
		for _, s := range append(append([]string{}, StateLabels...), Done, Dropped) {
			opts = append(opts, map[string]string{"id": "OPT_" + s, "name": s})
		}
		f.json(w, map[string]any{"data": map[string]any{"node": map[string]any{"field": map[string]any{"id": "FIELD_STATE", "options": opts}}}})
	case strings.Contains(q, "addProjectV2ItemById"):
		f.json(w, map[string]any{"data": map[string]any{"addProjectV2ItemById": map[string]any{"item": map[string]string{"id": "ITEM_" + vars["c"].(string)}}}})
	case strings.Contains(q, "updateProjectV2ItemFieldValue"):
		n, _ := strconv.Atoi(strings.TrimPrefix(vars["i"].(string), "ITEM_I_"))
		if is, ok := f.issues[n]; ok {
			is.BoardState = strings.TrimPrefix(vars["o"].(string), "OPT_")
		}
		f.writes = append(f.writes, "GRAPHQL board "+vars["i"].(string))
		f.json(w, map[string]any{"data": map[string]any{}})
	default:
		f.json(w, map[string]any{"errors": []map[string]string{{"message": "unknown query"}}})
	}
}
