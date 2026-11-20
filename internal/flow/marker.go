// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"fmt"
	"regexp"
)

// Marker is what the sync step writes into the body of each issue it opens.
// The sync step manages only issues that carry a marker.
type Marker struct {
	Plan string
	ID   string
	Hash string
}

var markerRe = regexp.MustCompile(`<!-- auto8-req plan=(\S+) id=(R[0-9]+) hash=([0-9a-f]{12}) -->`)

// MarkerText returns the HTML comment for r.
func MarkerText(r Requirement) string {
	return fmt.Sprintf("<!-- auto8-req plan=%s id=%s hash=%s -->", r.Plan, r.ID, r.Hash)
}

// ParseMarker finds the marker in an issue body.
func ParseMarker(body string) (Marker, bool) {
	m := markerRe.FindStringSubmatch(body)
	if m == nil {
		return Marker{}, false
	}
	return Marker{Plan: m[1], ID: m[2], Hash: m[3]}, true
}

// IssueTitle returns the title of the issue for r.
func IssueTitle(r Requirement) string {
	return r.Topic + " " + r.ID
}

// IssueBody returns the body of the issue for r. It links to the plan
// file on main and carries no requirement text.
func IssueBody(r Requirement, repo string) string {
	return fmt.Sprintf("Requirement %s of the plan [%s](https://github.com/%s/blob/main/%s).\n\nThe text of the requirement is in the plan file. Change it there, not here.\n\n%s\n",
		r.ID, r.Topic, repo, r.Plan, MarkerText(r))
}
