//go:build harness

// Package store is the harness build's SQLite database (docs/adr/0009): an append-only
// events log and the schema migrations later tables add to.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Zovaris/smithy/internal/ghx"
	_ "modernc.org/sqlite"
)

// Event is one row of the events log. Seq and Time are set by Append.
type Event struct {
	Seq     int64           `json:"seq"`
	Time    time.Time       `json:"time"`
	Kind    string          `json:"kind"`
	Session string          `json:"session,omitempty"`
	Data    json.RawMessage `json:"data"`
}

// Store is an open database.
type Store struct {
	db *sql.DB
}

// migrations run in order, once each; append new ones, never edit a shipped one.
var migrations = []string{
	`CREATE TABLE events (
		seq     INTEGER PRIMARY KEY AUTOINCREMENT,
		time    TEXT NOT NULL,
		kind    TEXT NOT NULL,
		session TEXT NOT NULL DEFAULT '',
		data    TEXT NOT NULL DEFAULT 'null'
	)`,
}

// DefaultPath is <smithy home>/smithy.db.
func DefaultPath() string { return filepath.Join(ghx.Home(), "smithy.db") }

// Open opens (creating if needed) the database at path in WAL mode and applies pending migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS migrations (version INTEGER PRIMARY KEY, applied TEXT NOT NULL)`); err != nil {
		return err
	}
	var v int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM migrations`).Scan(&v); err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf("store: database schema %d is newer than this smithy (%d); update smithy", v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		if _, err := tx.Exec(migrations[i]); err != nil {
			return fmt.Errorf("store: migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`INSERT INTO migrations (version, applied) VALUES (?, ?)`, i+1, now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Version is the number of migrations applied.
func (s *Store) Version() (int, error) {
	var v int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM migrations`).Scan(&v)
	return v, err
}

// Append writes e and returns its seq. Empty Data is stored as null; invalid JSON is refused.
func (s *Store) Append(e Event) (int64, error) {
	data := []byte(e.Data)
	if len(data) == 0 {
		data = []byte("null")
	}
	if !json.Valid(data) {
		return 0, errors.New("store: event data is not valid JSON")
	}
	res, err := s.db.Exec(`INSERT INTO events (time, kind, session, data) VALUES (?, ?, ?, ?)`, now(), e.Kind, e.Session, string(data))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Since returns every event with seq > after, oldest first.
func (s *Store) Since(after int64) ([]Event, error) {
	rows, err := s.db.Query(`SELECT seq, time, kind, session, data FROM events WHERE seq > ? ORDER BY seq`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var ts, data string
		if err := rows.Scan(&e.Seq, &ts, &e.Kind, &e.Session, &data); err != nil {
			return nil, err
		}
		if e.Time, err = time.Parse(time.RFC3339Nano, ts); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(data)
		out = append(out, e)
	}
	return out, rows.Err()
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
