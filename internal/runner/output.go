package runner

import (
	"regexp"
	"strings"
)

// RunOutput holds the structured sections of a worker's final output. Workers
// produce markdown with well-known ## headings; ParseRunOutput extracts each
// section's body so the UI can render them individually and the summary can be
// stored on the task for card-level display.
type RunOutput struct {
	Summary   string `json:"summary,omitempty"`
	Changes   string `json:"changes,omitempty"`
	Verify    string `json:"verify,omitempty"`
	Docs      string `json:"docs,omitempty"`
	Bootstrap string `json:"bootstrap,omitempty"`
	Raw       string `json:"raw"`
}

// sectionAliases maps normalized header text to a canonical RunOutput field.
var sectionAliases = map[string]string{
	"summary":            "summary",
	"changes":            "changes",
	"what changed":       "changes",
	"verification":       "verify",
	"verify":             "verify",
	"tests":              "verify",
	"level of effort":    "verify",
	"documentation":      "docs",
	"docs":               "docs",
	"worktree bootstrap": "bootstrap",
	"bootstrap":          "bootstrap",
}

// sectionHeaderRe matches ## headings and bare "Heading:" lines.
var sectionHeaderRe = regexp.MustCompile(`(?m)^(?:##\s+(.+)|([A-Z][A-Za-z ]+):)\s*$`)

// ParseRunOutput extracts well-known sections from worker output. It handles
// both `## Heading` (the canonical form) and `Heading:` (legacy). Unknown
// sections are left in Raw along with any preamble. The RESULT/WORKED_IN/etc
// trailer block is stripped from every section body.
func ParseRunOutput(text string) RunOutput {
	out := RunOutput{Raw: text}
	if text == "" {
		return out
	}

	type section struct {
		field       string
		headerStart int
		bodyStart   int
	}

	matches := sectionHeaderRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return out
	}

	var sections []section
	for _, m := range matches {
		var heading string
		if m[2] >= 0 {
			heading = text[m[2]:m[3]]
		} else if m[4] >= 0 {
			heading = text[m[4]:m[5]]
		}
		norm := strings.ToLower(strings.TrimSpace(heading))
		field, ok := sectionAliases[norm]
		if !ok {
			continue
		}
		sections = append(sections, section{field: field, headerStart: m[0], bodyStart: m[1]})
	}

	type resolved struct {
		field string
		body  string
	}
	var resolved_ []resolved
	for i, s := range sections {
		var limit int
		if i+1 < len(sections) {
			limit = sections[i+1].headerStart
		} else {
			limit = len(text)
		}
		limit = findSectionEnd(text, s.bodyStart, limit)
		body := cleanSectionBody(text[s.bodyStart:limit])
		resolved_ = append(resolved_, resolved{field: s.field, body: body})
	}

	for _, r := range resolved_ {
		if r.body == "" {
			continue
		}
		switch r.field {
		case "summary":
			out.Summary = r.body
		case "changes":
			out.Changes = r.body
		case "verify":
			if out.Verify != "" {
				out.Verify += "\n" + r.body
			} else {
				out.Verify = r.body
			}
		case "docs":
			out.Docs = r.body
		case "bootstrap":
			out.Bootstrap = r.body
		}
	}

	return out
}

var trailerLineRe = regexp.MustCompile(`(?m)^(RESULT|WORKED_IN|REPO|BRANCH|PR):[ \t].*$`)

func findSectionEnd(text string, bodyStart, limit int) int {
	loc := trailerLineRe.FindStringIndex(text[bodyStart:limit])
	if loc != nil {
		end := bodyStart + loc[0]
		if end < bodyStart {
			end = bodyStart
		}
		return end
	}
	return limit
}

func cleanSectionBody(s string) string {
	s = trailerLineRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	return s
}
