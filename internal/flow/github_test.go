// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDryRunWriteSendsNothing(t *testing.T) {
	f := newFake(t)
	f.addIssue(appLogin, "t R1", "b", Ready)
	c := f.client()
	var out bytes.Buffer
	c.DryRun, c.Out = true, &out
	if err := c.AddLabels(context.Background(), 1, Blocked); err != nil {
		t.Fatal(err)
	}
	if f.writeCount() != 0 {
		t.Fatalf("dry run sent %d writes", f.writeCount())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want one JSON line, got %q", out.String())
	}
	var op map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &op); err != nil || op["op"] != "add_labels" {
		t.Fatalf("bad JSON line %q: %v", lines[0], err)
	}
}

func TestErrorDoesNotHoldTheToken(t *testing.T) {
	f := newFake(t)
	c := f.client()
	f.failOn = "issues"
	_, err := c.ListIssues(context.Background())
	if err == nil {
		t.Fatalf("want an error")
	}
	if strings.Contains(err.Error(), fakeToken) {
		t.Fatalf("the error holds the token: %v", err)
	}
	c.Token = "wrong" + fakeToken
	f.failOn = ""
	_, err = c.ListIssues(context.Background())
	if err == nil || strings.Contains(err.Error(), fakeToken) {
		t.Fatalf("want an error without the token, got %v", err)
	}
}

func TestListIssuesReadsAllPages(t *testing.T) {
	f := newFake(t)
	for i := 0; i < 230; i++ {
		f.addIssue(appLogin, "t", "b")
	}
	got, err := f.client().ListIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 230 {
		t.Fatalf("got %d issues, want 230", len(got))
	}
}
