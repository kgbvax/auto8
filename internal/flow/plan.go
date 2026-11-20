// SPDX-License-Identifier: AGPL-3.0-or-later

// Package flow holds the workflow tool of auto8: it reads requirements from
// plan files and keeps one GitHub issue for each requirement.
package flow

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// PlansDir is the directory of the plan files, relative to the repository root.
const PlansDir = "docs/plans"

// UnifiedContract is the artifact_contract value of the plans that the tool reads.
const UnifiedContract = "ce-unified-plan/v1"

// Requirement is one requirement of a plan file.
type Requirement struct {
	Plan  string // path of the plan file, relative to the repository root
	Topic string // topic from the plan frontmatter
	ID    string // for example R3
	Text  string // the requirement text with white space normalized
	Hash  string // HashText(Text)
}

// Plan is one plan file with its requirements, in file order.
type Plan struct {
	Path         string
	Topic        string
	Requirements []Requirement
}

var reqLine = regexp.MustCompile(`^- (R[0-9]+)\. (.*)$`)

// HashText returns the first 12 hex digits of the SHA-256 of text,
// after all runs of white space are made into one space.
func HashText(text string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(text), " ")))
	return hex.EncodeToString(sum[:])[:12]
}

// frontmatter returns the key-value pairs between the first two "---" lines.
func frontmatter(data []byte) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return out
	}
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return out
}

// ParsePlan reads the requirements of one plan file. It returns ok false
// if the file does not carry the unified plan contract. The reports name
// problems that the operator must fix in the plan file.
func ParsePlan(p string, data []byte) (Plan, []string, bool) {
	fm := frontmatter(data)
	if fm["artifact_contract"] != UnifiedContract {
		return Plan{}, nil, false
	}
	plan := Plan{Path: p, Topic: fm["topic"]}
	var reports []string
	if plan.Topic == "" {
		return plan, []string{fmt.Sprintf("%s: the frontmatter has no topic", p)}, true
	}

	seen := map[string]bool{}
	var inProduct, inReqs bool
	var cur *Requirement
	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = strings.Join(strings.Fields(cur.Text), " ")
		cur.Hash = HashText(cur.Text)
		if seen[cur.ID] {
			reports = append(reports, fmt.Sprintf("%s: the ID %s is used more than once", p, cur.ID))
		} else {
			seen[cur.ID] = true
			plan.Requirements = append(plan.Requirements, *cur)
		}
		cur = nil
	}

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "## "):
			flush()
			inProduct = strings.TrimSpace(line[3:]) == "Product Contract"
			inReqs = false
			continue
		case strings.HasPrefix(line, "### "):
			flush()
			inReqs = inProduct && strings.TrimSpace(line[4:]) == "Requirements"
			continue
		}
		if !inReqs {
			continue
		}
		if m := reqLine.FindStringSubmatch(line); m != nil {
			flush()
			cur = &Requirement{Plan: p, Topic: plan.Topic, ID: m[1], Text: m[2]}
			continue
		}
		if cur != nil && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && strings.TrimSpace(line) != "" {
			cur.Text += " " + strings.TrimSpace(line)
			continue
		}
		flush()
	}
	flush()
	return plan, reports, true
}

// LoadPlans reads every unified plan file in dir of fsys. Two plans with
// the same topic are both left out and reported, because their issues
// would have the same titles.
func LoadPlans(fsys fs.FS, dir string) ([]Plan, []string) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", dir, err)}
	}
	var plans []Plan
	var reports []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := path.Join(dir, e.Name())
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			reports = append(reports, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		plan, r, ok := ParsePlan(p, data)
		reports = append(reports, r...)
		if ok && plan.Topic != "" {
			plans = append(plans, plan)
		}
	}
	byTopic := map[string][]string{}
	for _, pl := range plans {
		byTopic[pl.Topic] = append(byTopic[pl.Topic], pl.Path)
	}
	var out []Plan
	for _, pl := range plans {
		if len(byTopic[pl.Topic]) == 1 {
			out = append(out, pl)
		}
	}
	var dups []string
	for topic, paths := range byTopic {
		if len(paths) > 1 {
			dups = append(dups, fmt.Sprintf("the topic %s is in more than one plan: %s", topic, strings.Join(paths, ", ")))
		}
	}
	sort.Strings(dups)
	return out, append(reports, dups...)
}
