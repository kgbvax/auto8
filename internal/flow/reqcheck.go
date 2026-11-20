// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"bytes"
	"fmt"
	"io/fs"
	"os/exec"
	"sort"
	"strings"
	"testing/fstest"
)

// GitFS returns the files of dir at a git ref of the repository at root.
func GitFS(root, ref, dir string) (fs.FS, error) {
	out, err := exec.Command("git", "-C", root, "ls-tree", "-r", "--name-only", ref, "--", dir).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-tree %s %s: %v", ref, dir, err)
	}
	m := fstest.MapFS{}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name == "" {
			continue
		}
		data, err := exec.Command("git", "-C", root, "show", ref+":"+name).Output()
		if err != nil {
			return nil, fmt.Errorf("git show %s:%s: %v", ref, name, err)
		}
		m[name] = &fstest.MapFile{Data: data}
	}
	if len(m) == 0 {
		m[dir] = &fstest.MapFile{Mode: fs.ModeDir}
	}
	return m, nil
}

// reqSet maps "topic R3" to the hash of the requirement text.
func reqSet(plans []Plan) map[string]string {
	out := map[string]string{}
	for _, p := range plans {
		for _, r := range p.Requirements {
			out[IssueTitle(r)] = r.Hash
		}
	}
	return out
}

// CompareRequirements compares the requirements of the plans in dir of
// base and head. It returns one line for each difference and for each
// problem in the plan files of head. No lines means that the
// requirements did not change.
func CompareRequirements(base, head fs.FS, dir string) []string {
	bp, _ := LoadPlans(base, dir)
	hp, hreports := LoadPlans(head, dir)
	var out []string
	for _, r := range hreports {
		out = append(out, "plan problem: "+r)
	}
	b, h := reqSet(bp), reqSet(hp)
	var keys []string
	for k := range b {
		keys = append(keys, k)
	}
	for k := range h {
		if _, ok := b[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		bh, inB := b[k]
		hh, inH := h[k]
		switch {
		case inB && !inH:
			out = append(out, k+" is gone")
		case !inB && inH:
			out = append(out, k+" is new")
		case bh != hh:
			out = append(out, k+" has a changed text")
		}
	}
	return out
}

// CompareBuild compares only the requirements of one build between main
// and head. A change to another plan on main does not count. Problems in
// the plan files of head count, as in CompareRequirements.
func CompareBuild(main, head fs.FS, dir string, b BuildMarker) []string {
	mp, _ := LoadPlans(main, dir)
	hp, hreports := LoadPlans(head, dir)
	var out []string
	for _, r := range hreports {
		out = append(out, "plan problem: "+r)
	}
	hashes := func(plans []Plan) map[string]string {
		m := map[string]string{}
		for _, p := range plans {
			for _, r := range p.Requirements {
				m[key(r.Plan, r.ID)] = r.Hash
			}
		}
		return m
	}
	m, h := hashes(mp), hashes(hp)
	for _, id := range b.IDs {
		k := key(b.Plan, id)
		mh, inM := m[k]
		hh, inH := h[k]
		switch {
		case !inM:
			out = append(out, b.Plan+" "+id+" is not on main")
		case !inH:
			out = append(out, b.Plan+" "+id+" is gone in the pull request")
		case mh != hh:
			out = append(out, b.Plan+" "+id+" has a changed text")
		}
	}
	return out
}

// ReqCheck runs CompareRequirements for a pull request. It passes for an
// author other than app, because the operator changes requirements.
func ReqCheck(base, head fs.FS, dir, author, app string) []string {
	if author != app {
		return nil
	}
	return CompareRequirements(base, head, dir)
}

// FormatDiffs joins the difference lines for an error message.
func FormatDiffs(diffs []string) string {
	var b bytes.Buffer
	for _, d := range diffs {
		b.WriteString("- " + d + "\n")
	}
	return b.String()
}
