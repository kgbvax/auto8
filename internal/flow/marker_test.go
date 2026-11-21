// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"strings"
	"testing"
)

func TestMarkerRoundTrip(t *testing.T) {
	r := Requirement{Plan: "docs/plans/example.md", Topic: "example", ID: "R3", Text: "Text.", Hash: HashText("Text.")}
	body := IssueBody(r, "opsotter/auto8")
	m, ok := ParseMarker(body)
	if !ok {
		t.Fatalf("no marker found in %q", body)
	}
	if m.Plan != r.Plan || m.ID != r.ID || m.Hash != r.Hash {
		t.Fatalf("marker = %+v, want plan %s id %s hash %s", m, r.Plan, r.ID, r.Hash)
	}
	if !strings.Contains(body, "\n\n> "+r.Text+"\n\n") {
		t.Fatalf("the issue body must carry the requirement text as a block quote: %q", body)
	}
	if !strings.HasSuffix(body, MarkerText(r)+"\n") {
		t.Fatalf("the marker must be the last line: %q", body)
	}
	if !strings.Contains(body, "https://github.com/opsotter/auto8/blob/main/docs/plans/example.md") {
		t.Fatalf("the issue body must link to the plan file: %q", body)
	}
}

func TestBrokenMarkerReadsAsNoMarker(t *testing.T) {
	for _, body := range []string{
		"",
		"plain text",
		"<!-- auto8-req plan=docs/plans/x.md id=R3 -->",
		"<!-- auto8-req plan=docs/plans/x.md id=X3 hash=abcdefabcdef -->",
		"<!-- auto8-req plan=docs/plans/x.md id=R3 hash=xyz -->",
	} {
		if _, ok := ParseMarker(body); ok {
			t.Fatalf("body %q must not read as a marker", body)
		}
	}
}

func TestTitle(t *testing.T) {
	r := Requirement{Topic: "requirements-workflow", ID: "R3"}
	if got := IssueTitle(r); got != "requirements-workflow R3" {
		t.Fatalf("title = %q", got)
	}
	r.Name = "Jump to a high-value spot"
	if got := IssueTitle(r); got != "requirements-workflow R3: Jump to a high-value spot" {
		t.Fatalf("title with name = %q", got)
	}
}

func TestBodyWithName(t *testing.T) {
	r := Requirement{Plan: "docs/plans/example.md", Topic: "example", ID: "R18", Name: "Jump to a spot", Text: "When x, jump.", Hash: HashText("When x, jump.")}
	want := "Requirement R18 of the plan [example](https://github.com/opsotter/auto8/blob/main/docs/plans/example.md): Jump to a spot\n\n" +
		"> When x, jump.\n\n" + copyNote + "\n\n" + MarkerText(r) + "\n"
	if got := IssueBody(r, "opsotter/auto8"); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestTitleTopic(t *testing.T) {
	for title, want := range map[string]string{
		"example R3":          "example",
		"example R3: A name":  "example",
		"example R3: R3: odd": "example",
		"something else":      "something else",
	} {
		if got := titleTopic(title, "R3"); got != want {
			t.Fatalf("titleTopic(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestBodyFromSync(t *testing.T) {
	const repo = "opsotter/auto8"
	r := Requirement{Plan: "docs/plans/example.md", Topic: "example", ID: "R1", Name: "Old name", Text: "Old text.", Hash: HashText("Old text.")}
	m := Marker{Plan: r.Plan, ID: r.ID, Hash: r.Hash}
	noName := r
	noName.Name = ""
	for name, body := range map[string]string{
		"legacy":  legacyIssueBody(r, repo),
		"named":   IssueBody(r, repo),
		"no name": IssueBody(noName, repo),
	} {
		if !bodyFromSync(body, "example", repo, m) {
			t.Fatalf("%s: the body of the sync step reads as edited: %q", name, body)
		}
	}
	changed := strings.Replace(IssueBody(r, repo), "> Old text.", "> New text.", 1)
	for name, body := range map[string]string{
		"other topic":  IssueBody(r, repo),
		"changed text": changed,
		"added note":   IssueBody(r, repo) + "A note.\n",
	} {
		topic := "example"
		if name == "other topic" {
			topic = "other"
		}
		if bodyFromSync(body, topic, repo, m) {
			t.Fatalf("%s: an edited body reads as not edited: %q", name, body)
		}
	}
}
