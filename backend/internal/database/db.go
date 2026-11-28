package database

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	pragmas := url.Values{"_pragma": {
		"busy_timeout(5000)",
		"foreign_keys(1)",
		"journal_mode(WAL)",
		"synchronous(NORMAL)",
	}}
	dsn := url.URL{Scheme: "file", Opaque: ":memory:", RawQuery: pragmas.Encode()}
	if path != ":memory:" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
			return nil, err
		}
		dsn.Opaque = ""
		dsn.Path = filepath.ToSlash(abs)
	}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	return db, nil
}
