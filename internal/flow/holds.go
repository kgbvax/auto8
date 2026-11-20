// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"bufio"
	"context"
	"fmt"
	"path"
	"strings"
)

// ownerRule is one line of a CODEOWNERS file.
type ownerRule struct {
	pattern string
	owners  []string
}

// Owners holds the rules of a CODEOWNERS file.
type Owners struct {
	rules []ownerRule
}

// ParseOwners reads a CODEOWNERS file.
func ParseOwners(data string) Owners {
	var o Owners
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		o.rules = append(o.rules, ownerRule{pattern: f[0], owners: f[1:]})
	}
	return o
}

// matchOwner tells if a CODEOWNERS pattern matches file. It supports the
// forms that this repository uses: an anchored or a free file or
// directory name, and * inside one path part.
func matchOwner(pattern, file string) bool {
	anchored := strings.HasPrefix(pattern, "/")
	p := strings.TrimPrefix(pattern, "/")
	dir := strings.HasSuffix(p, "/")
	p = strings.TrimSuffix(p, "/")
	if p == "" || p == "*" {
		return true
	}
	if !anchored && !strings.Contains(p, "/") {
		for _, part := range strings.Split(file, "/") {
			if ok, _ := path.Match(p, part); ok {
				return true
			}
		}
		return false
	}
	fparts := strings.Split(file, "/")
	pparts := strings.Split(p, "/")
	if len(fparts) < len(pparts) {
		return false
	}
	for i, pp := range pparts {
		if ok, _ := path.Match(pp, fparts[i]); !ok {
			return false
		}
	}
	if len(fparts) == len(pparts) {
		return !dir
	}
	return true
}

// Owned tells if a file has a code owner. The last matching rule wins,
// as on GitHub. A rule without owners removes the owner.
func (o Owners) Owned(file string) bool {
	owned := false
	for _, r := range o.rules {
		if matchOwner(r.pattern, file) {
			owned = len(r.owners) > 0
		}
	}
	return owned
}

// Holds reads the changed files of pull request n. If an owned file
// changed, it sets needs-operator on each building issue of the build.
// It returns the owned files that changed.
func Holds(ctx context.Context, c *Client, app string, owners Owners, n int) ([]string, error) {
	pr, err := c.GetPull(ctx, n)
	if err != nil {
		return nil, err
	}
	if pr.Author != app {
		return nil, nil
	}
	b, ok := ParseBuildMarker(pr.Body)
	if !ok {
		return nil, fmt.Errorf("pull request #%d of the App has no build marker", n)
	}
	files, err := c.PullFiles(ctx, n)
	if err != nil {
		return nil, err
	}
	var owned []string
	for _, f := range files {
		if owners.Owned(f) {
			owned = append(owned, f)
		}
	}
	if len(owned) == 0 {
		return nil, nil
	}
	issues, err := openManaged(ctx, c, app)
	if err != nil {
		return owned, err
	}
	reason := fmt.Sprintf("Pull request #%d changes files that only the operator can release: %s. The operator must review and approve it.", n, strings.Join(owned, ", "))
	for _, id := range b.IDs {
		is, ok := issues[key(b.Plan, id)]
		if !ok {
			continue
		}
		if s, ok := StateOf(is); !ok || s != Building {
			continue
		}
		if err := c.SetState(ctx, &is, NeedsOperator, reason); err != nil {
			return owned, err
		}
	}
	return owned, nil
}
