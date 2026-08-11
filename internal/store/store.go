package store

import (
	"database/sql"
	"embed"
	"fmt"
	"strings"

	"github.com/Verhum/burnrate/internal/domain"
	_ "modernc.org/sqlite"
)

var (
	_ domain.TaskRepository         = (*Store)(nil)
	_ domain.RunRepository          = (*Store)(nil)
	_ domain.TaskPRRepository       = (*Store)(nil)
	_ domain.CommentRepository      = (*Store)(nil)
	_ domain.AttachmentRepository   = (*Store)(nil)
	_ domain.UsageRepository        = (*Store)(nil)
	_ domain.SettingsRepository     = (*Store)(nil)
	_ domain.HumanRequestRepository = (*Store)(nil)
	_ domain.CaptureRepository      = (*Store)(nil)
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %s: %w", pragma, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY
	)`)
	if err != nil {
		return err
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading migrations: %w", err)
	}

	for i, entry := range entries {
		ver := i + 1
		var exists int
		err := s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", ver).Scan(&exists)
		if err != nil {
			return err
		}
		if exists > 0 {
			continue
		}

		data, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("reading migration %d (%s): %w", ver, entry.Name(), err)
		}

		if err := s.execMigration(ver, string(data)); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) execMigration(ver int, migrationSQL string) error {
	stmts := splitStatements(migrationSQL)

	hasPragmaFK := false
	for _, stmt := range stmts {
		if strings.Contains(strings.ToUpper(stmt), "PRAGMA FOREIGN_KEYS") {
			hasPragmaFK = true
			break
		}
	}

	// PRAGMA foreign_keys cannot be changed inside a transaction, so
	// migrations that toggle it must run without a wrapping transaction.
	if hasPragmaFK {
		for _, stmt := range stmts {
			if _, err := s.db.Exec(stmt); err != nil {
				return fmt.Errorf("migration %d: %w", ver, err)
			}
		}
		if _, err := s.db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", ver); err != nil {
			return fmt.Errorf("migration %d: record version: %w", ver, err)
		}
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("migration %d: begin: %w", ver, err)
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", ver, err)
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", ver); err != nil {
		tx.Rollback()
		return fmt.Errorf("migration %d: record version: %w", ver, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migration %d: commit: %w", ver, err)
	}
	return nil
}

// splitStatements splits SQL text on semicolons while respecting single-quoted
// string literals, so that semicolons inside values are not treated as
// statement boundaries.
//
// `-- comments` are skipped whole. Migrations in this package carry long prose
// comments, and prose contains apostrophes: without this, one "doesn't" flips the
// quote state for the rest of the file and every subsequent semicolon stops being
// a boundary, so the migration reaches SQLite as one mangled statement. That
// failure looks like a syntax error pointing at an ordinary English word.
func splitStatements(sql string) []string {
	var stmts []string
	var current strings.Builder
	inQuote := false
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		if !inQuote && c == '-' && i+1 < len(sql) && sql[i+1] == '-' {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			// Keep the newline so statements stay separated in the output.
			current.WriteByte('\n')
			continue
		}
		if c == '\'' {
			if inQuote && i+1 < len(sql) && sql[i+1] == '\'' {
				current.WriteByte(c)
				current.WriteByte(c)
				i++
				continue
			}
			inQuote = !inQuote
			current.WriteByte(c)
		} else if c == ';' && !inQuote {
			s := strings.TrimSpace(current.String())
			if s != "" {
				stmts = append(stmts, s)
			}
			current.Reset()
		} else {
			current.WriteByte(c)
		}
	}
	s := strings.TrimSpace(current.String())
	if s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}
