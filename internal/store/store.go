// Package store is the panel's local SQLite database: file server agents,
// source AD profiles for imports and runtime settings. AD passwords are only ever
// stored sealed (the server's secret box): sync and source-profile passwords and the
// import credential.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	_ "modernc.org/sqlite" // pure-Go "sqlite" driver (no CGO)
)

// ErrNotFound reports that the record does not exist.
var ErrNotFound = errors.New("record not found")

// Agent is a file server running the ad-manager agent daemon.
type Agent struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Token    string    `json:"-"` // never leaves through the API
	Insecure bool      `json:"insecure"`
	Created  time.Time `json:"created"`
}

// Source is a saved source AD profile; its password, when kept, is in source_secrets (sealed) and only reported.
type Source struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Domain      string `json:"domain"`
	User        string `json:"user"`
	Insecure    bool   `json:"insecure"`
	HasPassword bool   `json:"has_password"`
}

// PasswordLink is a pending self-service password change. ID is a hash of the
// link token and Secret is the temporary password encrypted with a key derived
// from the token, so the database alone does not reveal it.
type PasswordLink struct {
	ID       string
	Sam      string
	Name     string
	Secret   []byte
	Expires  time.Time
	Attempts int
	Claim    string // hash of the browser secret set at the first access; empty until then
}

// Store wraps the SQLite connection.
type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS agents (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, url TEXT NOT NULL,
  token TEXT NOT NULL, insecure INTEGER NOT NULL DEFAULT 0, created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS ad_sources (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, url TEXT NOT NULL, domain TEXT NOT NULL,
  user TEXT NOT NULL DEFAULT '', insecure INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS source_secrets (source INTEGER PRIMARY KEY, secret BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS sync_secrets (
  agent INTEGER NOT NULL, job TEXT NOT NULL, secret BLOB NOT NULL, PRIMARY KEY (agent, job));
CREATE TABLE IF NOT EXISTS password_links (
  id TEXT PRIMARY KEY, sam TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', secret BLOB NOT NULL,
  expires TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, claim TEXT NOT NULL DEFAULT '');`

// RestoreSuffix names the database a panel restoration staged beside the current one (<db>.restore).
const RestoreSuffix = ".restore"

// CheckDB tells whether the database at path is one the panel can use: SQLite reads it whole, the panel's schema
// applies to it and its tables read (it changes the file: use it on a staged copy).
func CheckDB(path string) error {
	st, err := open(path)
	if err != nil {
		return fmt.Errorf("not a usable panel database: %w", err)
	}
	defer st.Close()
	var ok string
	if err := st.db.QueryRow("PRAGMA quick_check").Scan(&ok); err != nil || ok != "ok" {
		return fmt.Errorf("not a usable panel database: %v %s", err, ok)
	}
	// every table and column of the panel's schema (a table of the same name but other columns would pass the
	// schema's CREATE IF NOT EXISTS and fail later, half way through an action)
	want, err := schemaColumns(nil)
	if err != nil {
		return err
	}
	have, err := schemaColumns(st.db)
	if err != nil {
		return fmt.Errorf("not a usable panel database: %w", err)
	}
	for t, cols := range want {
		for _, c := range cols {
			if !slices.Contains(have[t], c) {
				return fmt.Errorf("not a usable panel database: %s.%s missing", t, c)
			}
		}
	}
	return nil
}

// schemaColumns lists the columns of each table of db (nil: of the panel's schema, in a database in memory).
func schemaColumns(db *sql.DB) (map[string][]string, error) {
	if db == nil {
		mem, err := sql.Open("sqlite", "file::memory:")
		if err != nil {
			return nil, err
		}
		defer mem.Close()
		mem.SetMaxOpenConns(1)
		if _, err := mem.Exec(schema); err != nil {
			return nil, err
		}
		db = mem
	}
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type = 'table'")
	if err != nil {
		return nil, err
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		tables = append(tables, n)
	}
	rows.Close()
	out := map[string][]string{}
	for _, t := range tables {
		cr, err := db.Query("SELECT name FROM pragma_table_info(?)", t)
		if err != nil {
			return nil, err
		}
		for cr.Next() {
			var c string
			if err := cr.Scan(&c); err != nil {
				cr.Close()
				return nil, err
			}
			out[t] = append(out[t], c)
		}
		cr.Close()
	}
	return out, nil
}

// applyPendingRestore puts a staged database in place before it is opened: the current one (with its WAL) is kept
// as <db>.before-restore. A staged database that does not check is left aside (<db>.restore-failed).
func applyPendingRestore(path string) error {
	staged := path + RestoreSuffix
	if _, err := os.Stat(staged); err != nil {
		return nil
	}
	if err := CheckDB(staged); err != nil {
		_ = os.Rename(staged, path+".restore-failed")
		return nil // the current database stays
	}
	for _, ext := range []string{"", "-wal"} {
		if _, err := os.Stat(path + ext); err == nil {
			if err := os.Rename(path+ext, path+".before-restore"+ext); err != nil {
				return fmt.Errorf("restored database: %w", err)
			}
		}
	}
	_ = os.Remove(path + "-shm")
	return os.Rename(staged, path)
}

// Open creates or opens the database with mode 0600 (it holds agent tokens); a database staged by a panel
// restoration goes in place first.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	if err := applyPendingRestore(path); err != nil {
		return nil, err
	}
	return open(path)
}

func open(path string) (*Store, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // ponytail: single writer; the panel's volume is low
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Agents lists the agents.
func (s *Store) Agents() ([]Agent, error) {
	rows, err := s.db.Query(`SELECT id, name, url, token, insecure, created FROM agents ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		var a Agent
		var created string
		if err := rows.Scan(&a.ID, &a.Name, &a.URL, &a.Token, &a.Insecure, &created); err != nil {
			return nil, err
		}
		a.Created, _ = time.Parse(time.RFC3339, created)
		out = append(out, a)
	}
	return out, rows.Err()
}

// Agent fetches one agent by ID.
func (s *Store) Agent(id int64) (*Agent, error) {
	var a Agent
	var created string
	err := s.db.QueryRow(`SELECT id, name, url, token, insecure, created FROM agents WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &a.URL, &a.Token, &a.Insecure, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	a.Created, _ = time.Parse(time.RFC3339, created)
	return &a, err
}

// SaveAgent inserts (ID 0) or updates an agent; an empty token on update keeps
// the current one.
func (s *Store) SaveAgent(a *Agent) error {
	if a.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO agents (name, url, token, insecure, created) VALUES (?, ?, ?, ?, ?)`,
			a.Name, a.URL, a.Token, a.Insecure, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return err
		}
		a.ID, err = res.LastInsertId()
		return err
	}
	_, err := s.db.Exec(`UPDATE agents SET name = ?, url = ?, insecure = ?, token = CASE WHEN ? = '' THEN token ELSE ? END WHERE id = ?`,
		a.Name, a.URL, a.Insecure, a.Token, a.Token, a.ID)
	return err
}

// DeleteAgent removes the registration (it does not touch the server).
func (s *Store) DeleteAgent(id int64) error {
	// its sync passwords go with it (a new agent may get the same id)
	if _, err := s.db.Exec(`DELETE FROM sync_secrets WHERE agent = ?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM agents WHERE id = ?`, id)
	return err
}

// Sources lists the source AD profiles.
func (s *Store) Sources() ([]Source, error) {
	rows, err := s.db.Query(`SELECT a.id, a.name, a.url, a.domain, a.user, a.insecure, x.source IS NOT NULL
		FROM ad_sources a LEFT JOIN source_secrets x ON x.source = a.id ORDER BY a.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		var x Source
		if err := rows.Scan(&x.ID, &x.Name, &x.URL, &x.Domain, &x.User, &x.Insecure, &x.HasPassword); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SaveSource saves a source profile, replacing any profile with the same name (it keeps its id), and sets x.ID.
func (s *Store) SaveSource(x *Source) error {
	_, err := s.db.Exec(`INSERT INTO ad_sources (name, url, domain, user, insecure) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET url = excluded.url, domain = excluded.domain, user = excluded.user, insecure = excluded.insecure`,
		x.Name, x.URL, x.Domain, x.User, x.Insecure)
	if err != nil {
		return err
	}
	return s.db.QueryRow(`SELECT id FROM ad_sources WHERE name = ?`, x.Name).Scan(&x.ID)
}

// Source reads one profile (ErrNotFound when it does not exist).
func (s *Store) Source(id int64) (*Source, error) {
	var x Source
	err := s.db.QueryRow(`SELECT a.id, a.name, a.url, a.domain, a.user, a.insecure, x.source IS NOT NULL
		FROM ad_sources a LEFT JOIN source_secrets x ON x.source = a.id WHERE a.id = ?`, id).
		Scan(&x.ID, &x.Name, &x.URL, &x.Domain, &x.User, &x.Insecure, &x.HasPassword)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, err
}

// DeleteSource removes a profile and its kept password.
func (s *Store) DeleteSource(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM source_secrets WHERE source = ?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM ad_sources WHERE id = ?`, id)
	return err
}

// SetSourceSecret keeps a profile's sealed password; nil deletes it.
func (s *Store) SetSourceSecret(id int64, sealed []byte) error {
	if sealed == nil {
		_, err := s.db.Exec(`DELETE FROM source_secrets WHERE source = ?`, id)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO source_secrets (source, secret) VALUES (?, ?)
		ON CONFLICT(source) DO UPDATE SET secret = excluded.secret`, id, sealed)
	return err
}

// SourceSecret reads a profile's sealed password (nil when none is kept).
func (s *Store) SourceSecret(id int64) ([]byte, error) {
	var v []byte
	err := s.db.QueryRow(`SELECT secret FROM source_secrets WHERE source = ?`, id).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// Setting reads a setting (ErrNotFound if it was never saved).
func (s *Store) Setting(key string) ([]byte, error) {
	var v []byte
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

// SetSetting saves a setting; a nil value deletes it.
func (s *Store) SetSetting(key string, value []byte) error {
	if value == nil {
		_, err := s.db.Exec(`DELETE FROM settings WHERE key = ?`, key)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// SavePasswordLink stores a link, replacing any other link of the same user and
// dropping expired ones.
func (s *Store) SavePasswordLink(l *PasswordLink) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`DELETE FROM password_links WHERE sam = ? COLLATE NOCASE OR expires <= ?`, l.Sam, now); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO password_links (id, sam, name, secret, expires) VALUES (?, ?, ?, ?, ?)`,
		l.ID, l.Sam, l.Name, l.Secret, l.Expires.UTC().Format(time.RFC3339))
	return err
}

// PasswordLink fetches a valid link; missing or expired is ErrNotFound.
func (s *Store) PasswordLink(id string) (*PasswordLink, error) {
	var l PasswordLink
	var exp string
	err := s.db.QueryRow(`SELECT id, sam, name, secret, expires, attempts, claim FROM password_links WHERE id = ? AND expires > ?`,
		id, time.Now().UTC().Format(time.RFC3339)).Scan(&l.ID, &l.Sam, &l.Name, &l.Secret, &exp, &l.Attempts, &l.Claim)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	l.Expires, _ = time.Parse(time.RFC3339, exp)
	return &l, err
}

// ClaimPasswordLink binds a link to the first browser that opens it and
// shortens its validity to until; false means another browser got it first.
func (s *Store) ClaimPasswordLink(id, claim string, until time.Time) (bool, error) {
	res, err := s.db.Exec(`UPDATE password_links SET claim = ?, expires = MIN(expires, ?) WHERE id = ? AND claim = ''`,
		claim, until.UTC().Format(time.RFC3339), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// PasswordLinkFailed counts a rejected attempt and returns the new total.
func (s *Store) PasswordLinkFailed(id string) (int, error) {
	var n int
	err := s.db.QueryRow(`UPDATE password_links SET attempts = attempts + 1 WHERE id = ? RETURNING attempts`, id).Scan(&n)
	return n, err
}

// DeletePasswordLink removes a link (used, exhausted or invalid).
func (s *Store) DeletePasswordLink(id string) error {
	_, err := s.db.Exec(`DELETE FROM password_links WHERE id = ?`, id)
	return err
}

// VacuumInto writes a consistent copy of the database to path (SQLite VACUUM INTO; path must not exist).
func (s *Store) VacuumInto(path string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, path)
	return err
}

// SetSyncSecret keeps the sealed password of a sync job of an agent.
func (s *Store) SetSyncSecret(agent int64, job string, sealed []byte) error {
	_, err := s.db.Exec(`INSERT INTO sync_secrets (agent, job, secret) VALUES (?, ?, ?)
		ON CONFLICT (agent, job) DO UPDATE SET secret = excluded.secret`, agent, job, sealed)
	return err
}

// SyncSecret returns the sealed password of a sync job (nil when there is none).
func (s *Store) SyncSecret(agent int64, job string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT secret FROM sync_secrets WHERE agent = ? AND job = ?`, agent, job).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, err
}

// DeleteSyncSecret forgets the password of a sync job.
func (s *Store) DeleteSyncSecret(agent int64, job string) error {
	_, err := s.db.Exec(`DELETE FROM sync_secrets WHERE agent = ? AND job = ?`, agent, job)
	return err
}
