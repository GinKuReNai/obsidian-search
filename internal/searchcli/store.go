package searchcli

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB          *sql.DB
	Vault, Path string
}

func Open(ctx context.Context, vault, path string) (*Store, error) {
	v, err := filepath.Abs(vault)
	if err != nil {
		return nil, err
	}
	v, err = filepath.EvalSymlinks(v)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	info, err := os.Stat(v)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("vault is not a directory")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err = f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	// A database alias could otherwise change a vault file's permissions or content.
	if !info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1 {
		f.Close()
		return nil, fmt.Errorf("database must be a regular file with a single hard link")
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Store, error) { db.Close(); return nil, e }
	// Initialization and vault binding share one transaction, including concurrent first opens.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fail(fmt.Errorf("initialize index: %w", err))
	}
	defer tx.Rollback()
	fail = func(e error) (*Store, error) { _ = tx.Rollback(); _ = db.Close(); return nil, e }
	var version int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version != 0 && version != 1 {
		return fail(fmt.Errorf("unsupported schema %d; use a compatible CLI", version))
	}
	if version == 0 {
		var tables int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
			return fail(err)
		}
		if tables != 0 {
			return fail(fmt.Errorf("refusing to initialize a non-empty, unrecognized database"))
		}
		_, err = tx.ExecContext(ctx, schema)
		if err != nil {
			return fail(fmt.Errorf("initialize FTS5/trigram: %w", err))
		}
	}
	var bound string
	err = tx.QueryRowContext(ctx, "SELECT value FROM meta WHERE key='vault'").Scan(&bound)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fail(fmt.Errorf("read vault binding: %w", err))
	}
	if bound != "" && bound != v {
		return fail(fmt.Errorf("index belongs to another vault"))
	}
	if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO meta VALUES('vault',?)", v); err != nil {
		return fail(err)
	}
	if err = tx.Commit(); err != nil {
		return fail(fmt.Errorf("initialize index commit: %w", err))
	}
	return &Store{db, v, path}, nil
}

// Read confines every path component with os.Root, including symlinks and races.
func (s *Store) Read(path string) ([]byte, error) {
	if filepath.IsAbs(path) || !filepath.IsLocal(path) || strings.Contains(path, "\\") {
		return nil, fmt.Errorf("invalid vault-relative path %q", path)
	}
	if strings.ToLower(filepath.Ext(path)) != ".md" {
		return nil, fmt.Errorf("only Markdown files are allowed")
	}
	root, err := os.OpenRoot(s.Vault)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	// Nonblocking open avoids hanging on a named pipe disguised as Markdown.
	f, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read %s: not a regular file", path)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if !utf8.Valid(b) {
		return nil, fmt.Errorf("read %s: invalid UTF-8", path)
	}
	return b, nil
}

type IndexStats struct {
	Added     int `json:"added"`
	Updated   int `json:"updated"`
	Deleted   int `json:"deleted"`
	Unchanged int `json:"unchanged"`
}

func (s *Store) Index(ctx context.Context, rebuild bool) (IndexStats, error) {
	stats := IndexStats{}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return stats, err
	}
	defer tx.Rollback()
	old := map[string]string{}
	rows, err := tx.QueryContext(ctx, "SELECT path,hash FROM files")
	if err != nil {
		return stats, err
	}
	for rows.Next() {
		var p, h string
		if err = rows.Scan(&p, &h); err != nil {
			rows.Close()
			return stats, err
		}
		old[p] = h
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return stats, err
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(s.Vault, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if d.IsDir() {
			if d.Name() == ".obsidian" || d.Name() == ".git" || d.Name() == ".trash" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if strings.ToLower(filepath.Ext(p)) != ".md" {
			return nil
		}
		rel, e := filepath.Rel(s.Vault, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		b, e := s.Read(rel)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(b)
		hash := hex.EncodeToString(sum[:])
		seen[rel] = true
		if old[rel] == hash && !rebuild {
			stats.Unchanged++
			return nil
		}
		sections, e := Parse(b)
		if e != nil {
			return fmt.Errorf("parse %s: %w", rel, e)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "DELETE FROM files WHERE path=?", rel); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO files VALUES(?,?,?,?)", rel, hash, info.ModTime().UnixNano(), len(b)); e != nil {
			return e
		}
		for _, sec := range sections {
			if _, e = tx.ExecContext(ctx, "INSERT INTO sections(path,heading,start,end,body) VALUES(?,?,?,?,?)", rel, sec.Heading, sec.Start, sec.End, sec.Body); e != nil {
				return e
			}
		}
		if _, ok := old[rel]; ok {
			stats.Updated++
		} else {
			stats.Added++
		}
		return nil
	})
	if err != nil {
		return IndexStats{}, fmt.Errorf("index rolled back: %w", err)
	}
	for p := range old {
		if !seen[p] {
			if _, err = tx.ExecContext(ctx, "DELETE FROM files WHERE path=?", p); err != nil {
				return IndexStats{}, err
			}
			stats.Deleted++
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO meta VALUES('updated',?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return IndexStats{}, err
	}
	if err = tx.Commit(); err != nil {
		return IndexStats{}, err
	}
	return stats, nil
}
