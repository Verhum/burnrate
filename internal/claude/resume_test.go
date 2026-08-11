package claude

import "testing"

func TestResumeCommand(t *testing.T) {
	tests := []struct {
		name   string
		target ResumeTarget
		want   string
	}{
		{
			name: "no session is not resumable",
			want: "",
		},
		{
			name:   "session only",
			target: ResumeTarget{SessionID: "sess-1"},
			want:   "claude --resume 'sess-1'",
		},
		{
			name:   "cd into the worktree so the session resolves",
			target: ResumeTarget{SessionID: "sess-1", WorkDir: "/tmp/agentwork/task-7"},
			want:   "cd '/tmp/agentwork/task-7' && claude --resume 'sess-1'",
		},
		{
			name: "pinned account carries its config dir",
			target: ResumeTarget{
				SessionID: "sess-1",
				WorkDir:   "/tmp/agentwork/task-7",
				ConfigDir: "/base/code/proj/.local_home/.claude",
			},
			want: "cd '/tmp/agentwork/task-7' && CLAUDE_CONFIG_DIR='/base/code/proj/.local_home/.claude' claude --resume 'sess-1'",
		},
		{
			// Without the token the CLI answers "Not logged in · Please run /login":
			// a pinned account's credentials live in a sandbox keychain it cannot read.
			name: "pinned account resolves a token at paste time",
			target: ResumeTarget{
				SessionID:    "sess-1",
				WorkDir:      "/tmp/agentwork/task-7",
				ConfigDir:    "/cfg",
				TokenCommand: "/usr/local/bin/burnrate token",
			},
			want: `cd '/tmp/agentwork/task-7' && CLAUDE_CONFIG_DIR='/cfg' CLAUDE_CODE_OAUTH_TOKEN="$(/usr/local/bin/burnrate token)" claude --resume 'sess-1'`,
		},
		{
			// An inherited-environment account finds its own credentials, so a token
			// command must not leak into a command that does not need one.
			name: "no config dir means no token substitution",
			target: ResumeTarget{
				SessionID:    "sess-1",
				WorkDir:      "/tmp/agentwork/task-7",
				TokenCommand: "/usr/local/bin/burnrate token",
			},
			want: "cd '/tmp/agentwork/task-7' && claude --resume 'sess-1'",
		},
		{
			name:   "config dir without a worktree",
			target: ResumeTarget{SessionID: "sess-1", ConfigDir: "/cfg"},
			want:   "CLAUDE_CONFIG_DIR='/cfg' claude --resume 'sess-1'",
		},
		{
			name:   "space in the path survives quoting",
			target: ResumeTarget{SessionID: "sess-1", WorkDir: "/tmp/my code/task-7"},
			want:   "cd '/tmp/my code/task-7' && claude --resume 'sess-1'",
		},
		{
			name:   "single quote in the path is escaped, not terminating",
			target: ResumeTarget{SessionID: "sess-1", WorkDir: "/tmp/alex's code"},
			want:   `cd '/tmp/alex'\''s code' && claude --resume 'sess-1'`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResumeCommand(tc.target)
			if got != tc.want {
				t.Errorf("ResumeCommand(%+v)\n got %q\nwant %q", tc.target, got, tc.want)
			}
		})
	}
}
