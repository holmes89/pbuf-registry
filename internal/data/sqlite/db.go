package sqlite

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	_ "modernc.org/sqlite"
)

// Open opens a *sql.DB for SQLite and enables WAL mode and foreign keys.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("could not open sqlite database: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("could not enable foreign keys: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("could not enable WAL mode: %w", err)
	}

	// Allow up to 5 s of retries when another process holds the write lock,
	// which happens when the registry server and background daemons run as
	// separate processes against the same SQLite file.
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("could not set busy timeout: %w", err)
	}

	return db, nil
}

// repo is an embedded helper used by all repository types in this package.
type repo struct {
	db     *sql.DB
	logger *log.Helper
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func fmtTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
