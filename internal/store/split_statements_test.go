package store

import "testing"

// Migrations in this package are mostly prose. A single apostrophe in a comment
// used to flip the quote state for the rest of the file, after which no semicolon
// counted as a statement boundary and the whole migration reached SQLite as one
// mangled statement — reported as a syntax error near an English word.
func TestSplitStatementsIgnoresComments(t *testing.T) {
	sql := `-- The author's note; with a semicolon and an apostrophe.
ALTER TABLE t ADD COLUMN c INTEGER NOT NULL DEFAULT 0;

-- Another one: doesn't; won't; can't.
UPDATE t SET c = 12 WHERE c = '' AND d != '';
`
	stmts := splitStatements(sql)
	if len(stmts) != 2 {
		t.Fatalf("got %d statements, want 2: %q", len(stmts), stmts)
	}
	if want := "ALTER TABLE t ADD COLUMN c INTEGER NOT NULL DEFAULT 0"; stmts[0] != want {
		t.Errorf("stmt 0 = %q, want %q", stmts[0], want)
	}
	if want := "UPDATE t SET c = 12 WHERE c = '' AND d != ''"; stmts[1] != want {
		t.Errorf("stmt 1 = %q, want %q", stmts[1], want)
	}
}

// The reason the quote tracking exists in the first place.
func TestSplitStatementsKeepsSemicolonsInsideLiterals(t *testing.T) {
	stmts := splitStatements(`INSERT INTO t VALUES ('a;b'); INSERT INTO t VALUES ('it''s');`)
	if len(stmts) != 2 {
		t.Fatalf("got %d statements, want 2: %q", len(stmts), stmts)
	}
	if want := "INSERT INTO t VALUES ('a;b')"; stmts[0] != want {
		t.Errorf("stmt 0 = %q, want %q", stmts[0], want)
	}
	if want := "INSERT INTO t VALUES ('it''s')"; stmts[1] != want {
		t.Errorf("stmt 1 = %q, want %q", stmts[1], want)
	}
}

// A `--` inside a string literal is data, not a comment.
func TestSplitStatementsDoesNotStripDashesInsideLiterals(t *testing.T) {
	stmts := splitStatements(`UPDATE t SET c = 'a -- b; c' WHERE id = 1;`)
	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(stmts), stmts)
	}
	if want := "UPDATE t SET c = 'a -- b; c' WHERE id = 1"; stmts[0] != want {
		t.Errorf("stmt = %q, want %q", stmts[0], want)
	}
}
