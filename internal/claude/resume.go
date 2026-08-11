package claude

import "strings"

// ResumeTarget describes a run well enough to reattach a terminal to its Claude
// session.
type ResumeTarget struct {
	SessionID string
	// WorkDir is the run's worktree. The CLI resolves a session id against the
	// current directory's project history, so the command has to cd there first.
	WorkDir string
	// ConfigDir is the CLAUDE_CONFIG_DIR the daemon spawned the agent with, which
	// is where the transcript was written. Empty for an account inherited from the
	// daemon's own environment, where no assignment is needed.
	ConfigDir string
	// TokenCommand is a shell command printing a fresh OAuth token for ConfigDir.
	// It is only used alongside ConfigDir, and only because a pinned account keeps
	// its credentials in a sandbox keychain the CLI cannot read: pointing the CLI
	// at the config dir alone gets "Not logged in · Please run /login", the same
	// reason runner.resolveTokenEnv injects CLAUDE_CODE_OAUTH_TOKEN. Empty leaves
	// the child to find its own credentials.
	TokenCommand string
}

// ResumeCommand builds the shell command a human types to reattach to a run's
// Claude session. Returns "" when the run recorded no session id — the same
// condition that makes its task unresumable by the daemon.
func ResumeCommand(t ResumeTarget) string {
	if t.SessionID == "" {
		return ""
	}
	var b strings.Builder
	if t.WorkDir != "" {
		b.WriteString("cd ")
		b.WriteString(shellQuote(t.WorkDir))
		b.WriteString(" && ")
	}
	if t.ConfigDir != "" {
		b.WriteString("CLAUDE_CONFIG_DIR=")
		b.WriteString(shellQuote(t.ConfigDir))
		b.WriteString(" ")
		if t.TokenCommand != "" {
			// Double-quoted substitution rather than a baked-in token: the secret
			// never lands in the clipboard, the API response, or shell history.
			b.WriteString(`CLAUDE_CODE_OAUTH_TOKEN="$(`)
			b.WriteString(t.TokenCommand)
			b.WriteString(`)" `)
		}
	}
	b.WriteString("claude --resume ")
	b.WriteString(shellQuote(t.SessionID))
	return b.String()
}

// shellQuote wraps s in single quotes so a path containing spaces survives being
// pasted into a shell. Any embedded single quote is closed, escaped, reopened.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
