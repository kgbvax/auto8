// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"context"
	"os"
	"strings"
	"testing"
)

func repoOwners(t *testing.T) Owners {
	t.Helper()
	data, err := os.ReadFile("../../.github/CODEOWNERS")
	if err != nil {
		t.Skipf("no CODEOWNERS file in this tree: %v", err)
	}
	return ParseOwners(string(data))
}

func TestOwnersOfThisRepository(t *testing.T) {
	o := repoOwners(t)
	for file, want := range map[string]bool{
		"internal/tx/x.go":                 true,
		".github/workflows/ci.yml":         true,
		".claude/settings.json":            true,
		"CLAUDE.md":                        true,
		"docs/SDLC.md":                     true,
		"internal/flow/sync.go":            true,
		"test/integration/peers/wsjtx.go":  true,
		"cmd/auto8/main.go":                false,
		"docs/plans/x.md":                  false,
		"internal/txt/x.go":                false,
		"internal/decode/CLAUDE.md":        false,
		"test/integration/feature_test.go": false,
	} {
		if got := o.Owned(file); got != want {
			t.Errorf("Owned(%s) = %v, want %v", file, got, want)
		}
	}
}

func TestOwnersPatterns(t *testing.T) {
	o := ParseOwners("*.lock @a\n/docs/ @b\n/docs/free.md\n")
	for file, want := range map[string]bool{
		"x/y.lock":     true,
		"docs/a.md":    true,
		"docs/free.md": false,
		"a/docs/b.md":  false,
	} {
		if got := o.Owned(file); got != want {
			t.Errorf("Owned(%s) = %v, want %v", file, got, want)
		}
	}
}

func holdsSetup(t *testing.T, files ...string) (*fakeGitHub, *fakePull) {
	t.Helper()
	plans := testPlan("R1", "One.")
	f := syncedIssues(t, plans)
	f.issue(1).Labels = []string{Building}
	pr := f.addPull(appLogin, BuildMarkerText("docs/plans/example.md", []string{"R1"}), "sha1", files...)
	return f, pr
}

func TestHoldsSetsNeedsOperatorForTXControl(t *testing.T) {
	f, pr := holdsSetup(t, "internal/tx/x.go", "cmd/auto8/main.go")
	owned, err := Holds(context.Background(), f.client(), appLogin, ParseOwners("/internal/tx/ @kgbvax\n"), pr.Number)
	if err != nil || len(owned) != 1 {
		t.Fatalf("holds = %v, %v", owned, err)
	}
	if got := f.issue(1).Labels; len(got) != 1 || got[0] != NeedsOperator {
		t.Fatalf("labels = %v", got)
	}
}

func TestHoldsLeavesOtherChangesAlone(t *testing.T) {
	f, pr := holdsSetup(t, "cmd/auto8/main.go")
	owned, err := Holds(context.Background(), f.client(), appLogin, ParseOwners("/internal/tx/ @kgbvax\n"), pr.Number)
	if err != nil || len(owned) != 0 {
		t.Fatalf("holds = %v, %v", owned, err)
	}
	if got := f.issue(1).Labels; got[0] != Building {
		t.Fatalf("labels = %v", got)
	}
}

func TestHoldsIgnoresAnotherAuthor(t *testing.T) {
	f, _ := holdsSetup(t)
	pr := f.addPull("visitor", BuildMarkerText("docs/plans/example.md", []string{"R1"}), "sha2", "internal/tx/x.go")
	owned, err := Holds(context.Background(), f.client(), appLogin, ParseOwners("/internal/tx/ @kgbvax\n"), pr.Number)
	if err != nil || owned != nil || f.issue(1).Labels[0] != Building {
		t.Fatalf("holds = %v, %v, labels = %v", owned, err, f.issue(1).Labels)
	}
}

func TestHoldsFailsForARequirementWithoutAnIssue(t *testing.T) {
	f, _ := holdsSetup(t)
	pr := f.addPull(appLogin, BuildMarkerText("docs/plans/example.md", []string{"R1", "R9"}), "sha2", "internal/tx/x.go")
	if _, err := Holds(context.Background(), f.client(), appLogin, ParseOwners("/internal/tx/ @kgbvax\n"), pr.Number); err == nil || !strings.Contains(err.Error(), "R9") {
		t.Fatalf("want an error that names R9, got %v", err)
	}
}
