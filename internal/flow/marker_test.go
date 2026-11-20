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
	if strings.Contains(body, r.Text) {
		t.Fatalf("the issue body must not carry the requirement text")
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
}
