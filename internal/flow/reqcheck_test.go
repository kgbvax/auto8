// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"strings"
	"testing"
	"testing/fstest"
)

func planFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for k, v := range files {
		m["docs/plans/"+k] = &fstest.MapFile{Data: []byte(v)}
	}
	return m
}

func TestReqCheckFailsWhenTheAppSplitsARequirement(t *testing.T) {
	base := planFS(map[string]string{"a.md": unifiedPlan})
	head := planFS(map[string]string{"a.md": strings.Replace(unifiedPlan, "- R3. The third requirement.", "- R3. The third.\n- R33. Requirement.", 1)})
	diffs := ReqCheck(base, head, "docs/plans", appLogin, appLogin)
	if len(diffs) != 2 {
		t.Fatalf("diffs = %v", diffs)
	}
}

func TestReqCheckPassesForAPlanningContract(t *testing.T) {
	base := planFS(map[string]string{"a.md": unifiedPlan})
	head := planFS(map[string]string{"a.md": unifiedPlan + "\n### Key Technical Decisions\n\n- KTD1. A decision.\n"})
	if diffs := ReqCheck(base, head, "docs/plans", appLogin, appLogin); len(diffs) != 0 {
		t.Fatalf("diffs = %v", diffs)
	}
}

func TestReqCheckPassesForTheOperator(t *testing.T) {
	base := planFS(map[string]string{"a.md": unifiedPlan})
	head := planFS(map[string]string{"a.md": strings.Replace(unifiedPlan, "first", "changed", 1)})
	if diffs := ReqCheck(base, head, "docs/plans", operator, appLogin); len(diffs) != 0 {
		t.Fatalf("diffs = %v", diffs)
	}
	if diffs := ReqCheck(base, head, "docs/plans", appLogin, appLogin); len(diffs) != 1 {
		t.Fatalf("for the App, want one diff, got %v", diffs)
	}
}

func TestReqCheckFailsWhenTheAppChangesAName(t *testing.T) {
	named := strings.Replace(unifiedPlan, "- R1. The first", "- R1. **A name.** The first", 1)
	renamed := strings.Replace(named, "**A name.**", "**Another name.**", 1)
	for _, c := range []struct{ name, base, head string }{
		{"add", unifiedPlan, named},
		{"change", named, renamed},
	} {
		base := planFS(map[string]string{"a.md": c.base})
		head := planFS(map[string]string{"a.md": c.head})
		diffs := ReqCheck(base, head, "docs/plans", appLogin, appLogin)
		if len(diffs) != 1 || !strings.Contains(diffs[0], "changed name") {
			t.Fatalf("%s by the App: diffs = %v", c.name, diffs)
		}
		if diffs := ReqCheck(base, head, "docs/plans", operator, appLogin); len(diffs) != 0 {
			t.Fatalf("%s by the operator: diffs = %v", c.name, diffs)
		}
	}
}

func TestReqCheckFailsWhenTheAppChangesATopic(t *testing.T) {
	base := planFS(map[string]string{"a.md": unifiedPlan})
	head := planFS(map[string]string{"a.md": strings.Replace(unifiedPlan, "topic: example", "topic: renamed", 1)})
	diffs := ReqCheck(base, head, "docs/plans", appLogin, appLogin)
	if len(diffs) != 3 || !strings.Contains(diffs[0], "changed topic") {
		t.Fatalf("diffs = %v", diffs)
	}
}

func TestReqCheckFailsForADuplicateTopic(t *testing.T) {
	base := planFS(map[string]string{"a.md": unifiedPlan})
	head := planFS(map[string]string{"a.md": unifiedPlan, "b.md": unifiedPlan})
	diffs := ReqCheck(base, head, "docs/plans", appLogin, appLogin)
	if len(diffs) == 0 || !strings.Contains(diffs[0], "plan problem") {
		t.Fatalf("diffs = %v", diffs)
	}
}

func TestReqCheckFailsWhenTheContractIsRemoved(t *testing.T) {
	base := planFS(map[string]string{"a.md": unifiedPlan})
	head := planFS(map[string]string{"a.md": strings.Replace(unifiedPlan, "artifact_contract: ce-unified-plan/v1\n", "", 1)})
	if diffs := ReqCheck(base, head, "docs/plans", appLogin, appLogin); len(diffs) != 3 {
		t.Fatalf("diffs = %v", diffs)
	}
}

func TestReqCheckAgainstCurrentMain(t *testing.T) {
	base := planFS(map[string]string{"a.md": unifiedPlan})
	main := planFS(map[string]string{"a.md": strings.Replace(unifiedPlan, "The third requirement.", "The third requirement, changed.", 1)})
	head := base
	if diffs := ReqCheck(base, head, "docs/plans", appLogin, appLogin); len(diffs) != 0 {
		t.Fatalf("head against base: %v", diffs)
	}
	if diffs := ReqCheck(main, head, "docs/plans", appLogin, appLogin); len(diffs) != 1 {
		t.Fatalf("head against main: %v", diffs)
	}
}

func TestGitFSReadsThePlansOfHEAD(t *testing.T) {
	fsys, err := GitFS("../..", "HEAD", "docs/plans")
	if err != nil {
		t.Skipf("no git: %v", err)
	}
	plans, reports := LoadPlans(fsys, "docs/plans")
	if len(reports) != 0 || len(plans) == 0 {
		t.Fatalf("plans = %d, reports = %v", len(plans), reports)
	}
}
