package session

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// StateDB wraps a SQLite database for session persistence.
type StateDB struct {
	db *sql.DB
}

// SessionRow represents a session row in the database.
type SessionRow struct {
	ID              string
	Title           string
	ProjectPath     string
	Status          string
	TmuxSession     string
	CreatedAt       time.Time
	LastAccessed    time.Time
	Acknowledged    bool
	ClaudeSessionID string
	WorkspaceName   string
}

// DefaultDBPath returns the default database path.
func DefaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "brizz-code", "state.db")
}

// Open opens or creates the SQLite database with WAL mode.
func Open(dbPath string) (*StateDB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Configure for concurrent access.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("set pragma %q: %w", p, err)
		}
	}

	s := &StateDB{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// Close checkpoints the WAL and closes the database.
func (s *StateDB) Close() error {
	_, _ = s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return s.db.Close()
}

func (s *StateDB) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id            TEXT PRIMARY KEY,
			title         TEXT NOT NULL,
			project_path  TEXT NOT NULL,
			status        TEXT NOT NULL DEFAULT 'idle',
			tmux_session  TEXT NOT NULL DEFAULT '',
			created_at    INTEGER NOT NULL,
			last_accessed INTEGER NOT NULL DEFAULT 0,
			acknowledged  INTEGER NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		return err
	}

	// Add claude_session_id column if missing.
	if !s.hasColumn("sessions", "claude_session_id") {
		_, err = s.db.Exec(`ALTER TABLE sessions ADD COLUMN claude_session_id TEXT NOT NULL DEFAULT ''`)
		if err != nil {
			return err
		}
	}

	// Add workspace_name column if missing.
	if !s.hasColumn("sessions", "workspace_name") {
		_, err = s.db.Exec(`ALTER TABLE sessions ADD COLUMN workspace_name TEXT NOT NULL DEFAULT ''`)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *StateDB) hasColumn(table, column string) bool {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue *string
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			continue
		}
		if name == column {
			return true
		}
	}
	return false
}

// SaveSession inserts or replaces a session row.
func (s *StateDB) SaveSession(row *SessionRow) error {
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO sessions (id, title, project_path, status, tmux_session, created_at, last_accessed, acknowledged, claude_session_id, workspace_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		row.ID, row.Title, row.ProjectPath, row.Status, row.TmuxSession,
		row.CreatedAt.Unix(), row.LastAccessed.Unix(), boolToInt(row.Acknowledged),
		row.ClaudeSessionID, row.WorkspaceName,
	)
	return err
}

// LoadSessions returns all sessions ordered by creation time.
func (s *StateDB) LoadSessions() ([]*SessionRow, error) {
	rows, err := s.db.Query(`
		SELECT id, title, project_path, status, tmux_session, created_at, last_accessed, acknowledged, claude_session_id, workspace_name
		FROM sessions ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*SessionRow
	for rows.Next() {
		var r SessionRow
		var createdAt, lastAccessed int64
		var ack int
		if err := rows.Scan(&r.ID, &r.Title, &r.ProjectPath, &r.Status, &r.TmuxSession, &createdAt, &lastAccessed, &ack, &r.ClaudeSessionID, &r.WorkspaceName); err != nil {
			return nil, err
		}
		r.CreatedAt = time.Unix(createdAt, 0)
		r.LastAccessed = time.Unix(lastAccessed, 0)
		r.Acknowledged = ack != 0
		sessions = append(sessions, &r)
	}
	return sessions, rows.Err()
}

// DeleteSession removes a session by ID.
func (s *StateDB) DeleteSession(id string) error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	return err
}

// UpdateStatus updates the status field and auto-clears acknowledged on "running".
func (s *StateDB) UpdateStatus(id, status string) error {
	_, err := s.db.Exec(`
		UPDATE sessions SET status = ?,
			acknowledged = CASE WHEN ? = 'running' THEN 0 ELSE acknowledged END
		WHERE id = ?
	`, status, status, id)
	return err
}

// SetAcknowledged updates the acknowledged flag.
func (s *StateDB) SetAcknowledged(id string, ack bool) error {
	_, err := s.db.Exec("UPDATE sessions SET acknowledged = ? WHERE id = ?", boolToInt(ack), id)
	return err
}

// UpdateLastAccessed updates the last_accessed timestamp.
func (s *StateDB) UpdateLastAccessed(id string) error {
	_, err := s.db.Exec("UPDATE sessions SET last_accessed = ? WHERE id = ?", time.Now().Unix(), id)
	return err
}

// UpdateTmuxSession updates the tmux session name (used after restart).
func (s *StateDB) UpdateTmuxSession(id, tmuxSession string) error {
	_, err := s.db.Exec("UPDATE sessions SET tmux_session = ? WHERE id = ?", tmuxSession, id)
	return err
}

// UpdateClaudeSessionID updates the Claude conversation session ID.
func (s *StateDB) UpdateClaudeSessionID(id, claudeSessionID string) error {
	_, err := s.db.Exec("UPDATE sessions SET claude_session_id = ? WHERE id = ?", claudeSessionID, id)
	return err
}

// UpdateTitle updates the session title.
func (s *StateDB) UpdateTitle(id, title string) error {
	_, err := s.db.Exec("UPDATE sessions SET title = ? WHERE id = ?", title, id)
	return err
}

// UpdateWorkspaceName updates the workspace name for a session.
func (s *StateDB) UpdateWorkspaceName(id, name string) error {
	_, err := s.db.Exec("UPDATE sessions SET workspace_name = ? WHERE id = ?", name, id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
