// SPDX-License-Identifier: AGPL-3.0-or-later

package flow

import (
	"fmt"
	"regexp"
	"strings"
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

// IssueTitle returns the title of the issue for r: "topic R3: Name",
// or "topic R3" if r has no name.
func IssueTitle(r Requirement) string {
	if r.Name != "" {
		return r.Topic + " " + r.ID + ": " + r.Name
	}
	return r.Topic + " " + r.ID
}

// copyNote is the sentence in the issue body that names the source of the text.
const copyNote = "This text is a copy. The plan file is its source, and the sync step updates this copy."

// bodyHead returns the first part of the first line of the issue body.
func bodyHead(r Requirement, repo string) string {
	return fmt.Sprintf("Requirement %s of the plan [%s](https://github.com/%s/blob/main/%s)", r.ID, r.Topic, repo, r.Plan)
}

// IssueBody returns the body of the issue for r. It names the
// requirement, links to the plan file on main, and carries a copy of the
// requirement text as a block quote. The last line is the marker.
func IssueBody(r Requirement, repo string) string {
	head := bodyHead(r, repo) + "."
	if r.Name != "" {
		head = bodyHead(r, repo) + ": " + r.Name
	}
	return fmt.Sprintf("%s\n\n> %s\n\n%s\n\n%s\n", head, r.Text, copyNote, MarkerText(r))
}

// legacyIssueBody returns the body that the sync step wrote before the
// body carried the requirement text. The sync step reads it as not
// edited and writes the new body in its place.
func legacyIssueBody(r Requirement, repo string) string {
	return fmt.Sprintf("Requirement %s of the plan [%s](https://github.com/%s/blob/main/%s).\n\nThe text of the requirement is in the plan file. Change it there, not here.\n\n%s\n",
		r.ID, r.Topic, repo, r.Plan, MarkerText(r))
}

// titleTopic returns the topic that the title of an issue for the
// requirement id names. The title is "topic R3" or "topic R3: Name".
func titleTopic(title, id string) string {
	if t, ok := strings.CutSuffix(title, " "+id); ok {
		return t
	}
	if i := strings.Index(title, " "+id+": "); i >= 0 {
		return title[:i]
	}
	return title
}

// bodyFromSync reports whether body is a body that the sync step wrote,
// in the old or in the new format. The marker gives the plan, the ID and
// the hash. topic is the topic from the title. For the new format, the
// name and the text come from the body itself. The text must give the
// hash of the marker, and IssueBody must give exactly the body again.
func bodyFromSync(body, topic, repo string, m Marker) bool {
	r := Requirement{Plan: m.Plan, Topic: topic, ID: m.ID, Hash: m.Hash}
	if body == legacyIssueBody(r, repo) {
		return true
	}
	lines := strings.SplitN(body, "\n", 4)
	if len(lines) < 4 || lines[1] != "" {
		return false
	}
	rest, ok := strings.CutPrefix(lines[0], bodyHead(r, repo))
	if !ok {
		return false
	}
	if name, ok := strings.CutPrefix(rest, ": "); ok {
		r.Name = name
	} else if rest != "." {
		return false
	}
	text, ok := strings.CutPrefix(lines[2], "> ")
	if !ok || HashText(text) != m.Hash {
		return false
	}
	r.Text = text
	return body == IssueBody(r, repo)
}
