package runner

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/prompts"
)

// Level of effort is how far a worker carries a task before calling it done —
// investigate only, land code, prove it with tests, or validate the whole path
// end to end. It is unrelated to the model's reasoning effort.
const (
	EffortInvestigate = 1
	EffortImplement   = 2
	EffortVerify      = 3
	EffortValidate    = 4
)

// DefaultEffortLevel is where every task lands unless the user says otherwise:
// implemented, tested, and shown to work. EffortValidate is reachable only from
// an explicit user directive — a worker is never told to promote itself to it.
const DefaultEffortLevel = EffortVerify

var effortLabels = map[int]string{
	EffortInvestigate: "Investigate",
	EffortImplement:   "Write the code",
	EffortVerify:      "Verify",
	EffortValidate:    "Validate end to end",
}

// effortDirective matches an explicit level a user wrote into a task
// description or a follow-up comment: "LOE: 4", "level of effort 1",
// "effort level = investigate". The keyword is required and the value must
// follow it on the same line — a bare "level 4" elsewhere in a description is
// far more likely to be prose than a directive.
var effortDirective = regexp.MustCompile(`(?i)\b(?:loe|levels?\s+of\s+effort|effort\s+levels?)\b[ \t]*(?:is|at|=|:|-|—)?[ \t]*(?:level[ \t]*)?(\d+|[a-z][a-z ]*)`)

// effortNames maps the word forms of a level onto its number. Only the leading
// word of the captured phrase is considered, so "verify the code using
// reasonable logic" still resolves to 3.
var effortNames = map[string]int{
	"investigate":    EffortInvestigate,
	"investigation":  EffortInvestigate,
	"research":       EffortInvestigate,
	"implement":      EffortImplement,
	"implementation": EffortImplement,
	"write":          EffortImplement,
	"code":           EffortImplement,
	"verify":         EffortVerify,
	"verified":       EffortVerify,
	"verification":   EffortVerify,
	"test":           EffortVerify,
	"validate":       EffortValidate,
	"validation":     EffortValidate,
	"integration":    EffortValidate,
	"integrate":      EffortValidate,
}

// parseEffortLevel finds the last explicit level directive in text. The last
// one wins: a description that explains the scale before naming its level, or a
// user who corrected themselves further down, should end at the level they
// finished on.
func parseEffortLevel(text string) (int, bool) {
	level, found := 0, false
	for _, m := range effortDirective.FindAllStringSubmatch(text, -1) {
		if lvl, ok := effortValue(m[1]); ok {
			level, found = lvl, true
		}
	}
	return level, found
}

// effortValue resolves one captured directive argument — "4", "investigate",
// "verify the code" — to a level, rejecting anything that isn't one of the four.
func effortValue(arg string) (int, bool) {
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		return 0, false
	}
	if arg[0] >= '0' && arg[0] <= '9' {
		if len(arg) == 1 && arg[0] >= '1' && arg[0] <= '4' {
			return int(arg[0] - '0'), true
		}
		return 0, false
	}
	word, _, _ := strings.Cut(arg, " ")
	lvl, ok := effortNames[word]
	return lvl, ok
}

// resolveEffortLevel picks the level for a run. Follow-up comments outrank the
// task description — they are the user's latest word — and the newest comment
// that names a level outranks older ones. With no directive anywhere, the
// default applies and the worker is told to judge from there.
func resolveEffortLevel(taskPrompt string, comments []store.Comment) (int, bool) {
	for i := len(comments) - 1; i >= 0; i-- {
		if lvl, ok := parseEffortLevel(comments[i].Body); ok {
			return lvl, true
		}
	}
	if lvl, ok := parseEffortLevel(taskPrompt); ok {
		return lvl, true
	}
	return DefaultEffortLevel, false
}

// effortSection renders the level-of-effort instructions for a prompt: the
// shared description of the four levels, then the level this run must work at.
func effortSection(level int, explicit bool) string {
	var sb strings.Builder
	sb.WriteString(prompts.EffortLevels)
	sb.WriteString("\n")
	if explicit {
		fmt.Fprintf(&sb, "**Level for this run: %d — %s.** The task explicitly asks for this level. "+
			"Honor it: do not stop short of it, and do not quietly expand past it. If you believe it is "+
			"wrong for this change, do the work at the requested level and say why in your final output.\n",
			level, effortLabels[level])
		return sb.String()
	}
	fmt.Fprintf(&sb, "**Level for this run: %d — %s (default).** No level was requested, so the default "+
		"applies. Treat a pure research ask as a 1; otherwise work at 3. **Never raise yourself to 4** — "+
		"the user did not ask for it, so no property of the change earns it. If you think this one deserves "+
		"end-to-end validation, do it at 3 and say so in your final output.\n",
		level, effortLabels[level])
	return sb.String()
}

// effortLine is the one-line form used where a full section would be noise —
// the auto-continue nudge, where the session already carries the instructions.
func effortLine(level int, explicit bool) string {
	suffix := " (default)"
	if explicit {
		suffix = " (requested by the task)"
	}
	return fmt.Sprintf("- LEVEL OF EFFORT: %d — %s%s\n", level, effortLabels[level], suffix)
}
