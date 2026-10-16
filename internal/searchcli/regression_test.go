package searchcli

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRejectForeignAndFutureDatabase(t *testing.T) {
	for _, tt := range []struct{ name, setup string }{{"foreign", "CREATE TABLE personal_data(value TEXT); INSERT INTO personal_data VALUES('keep')"}, {"future", "PRAGMA user_version=99"}} {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			path := filepath.Join(base, "index.db")
			db, e := sql.Open("sqlite", path)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = db.Exec(tt.setup); e != nil {
				t.Fatal(e)
			}
			if e = db.Close(); e != nil {
				t.Fatal(e)
			}
			if s, e := Open(context.Background(), base, path); e == nil {
				s.DB.Close()
				t.Fatal("unrecognized DB accepted")
			}
			db, e = sql.Open("sqlite", path)
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			var n int
			if e = db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='meta'").Scan(&n); e != nil || n != 0 {
				t.Fatalf("database modified %d %v", n, e)
			}
			if tt.name == "foreign" {
				var value string
				if e = db.QueryRow("SELECT value FROM personal_data").Scan(&value); e != nil || value != "keep" {
					t.Fatal("foreign content changed")
				}
			}
		})
	}
}
func TestConcurrentFirstOpenVaultBinding(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "index.db")
	vaults := []string{filepath.Join(base, "a"), filepath.Join(base, "b")}
	for _, v := range vaults {
		if e := os.Mkdir(v, 0700); e != nil {
			t.Fatal(e)
		}
	}
	results := make(chan error, 2)
	for _, v := range vaults {
		go func(v string) {
			s, e := Open(context.Background(), v, path)
			if e == nil {
				e = s.DB.Close()
			}
			results <- e
		}(v)
	}
	successes := 0
	for range 2 {
		if e := <-results; e == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("%d vaults bound to one index", successes)
	}
}
func TestSpecialFileRejected(t *testing.T) {
	s, v := fixture(t)
	if e := syscall.Mkfifo(filepath.Join(v, "pipe.md"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Read("pipe.md"); e == nil {
		t.Fatal("named pipe accepted")
	}
	if stats, e := s.Index(context.Background(), false); e != nil || stats.Added != 0 {
		t.Fatalf("%+v %v", stats, e)
	}
}

func TestDatabaseHardLinkPreservesSource(t *testing.T) {
	base := t.TempDir()
	vault := filepath.Join(base, "vault")
	if err := os.Mkdir(vault, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(vault, "note.md")
	want := []byte("# Private note\nkeep unchanged\n")
	if err := os.WriteFile(source, want, 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "index.db")
	if err := os.Link(source, alias); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"index", "--vault", vault, "--db", alias}, &out, &stderr); code != 1 {
		t.Fatalf("hard-linked database accepted: %d %s", code, stderr.String())
	}
	after, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(got, want) || after.Mode() != before.Mode() {
		t.Fatalf("source content or permissions changed: %q %v", got, err)
	}
}

func TestDatabaseSpecialFileRejected(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "pipe.db")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(context.Background(), base, path); err == nil {
		s.DB.Close()
		t.Fatal("named pipe accepted as database")
	}
}
func TestShowIndependentOfDatabase(t *testing.T) {
	_, v := fixture(t)
	put(t, v, "a.md", "# Heading\ntext")
	var out, stderr bytes.Buffer
	args := []string{"show", "a.md", "--vault", v, "--db", filepath.Join(v, "must-not-be-used.db"), "--format", "json"}
	if c := Run(context.Background(), args, &out, &stderr); c != 0 {
		t.Fatalf("%d %s", c, stderr.String())
	}
	if _, e := os.Stat(filepath.Join(v, "must-not-be-used.db")); !os.IsNotExist(e) {
		t.Fatal("show created database")
	}
	out.Reset()
	stderr.Reset()
	args = append(args, "--section", "Heading", "--start-line", "1")
	if c := Run(context.Background(), args, &out, &stderr); c != 2 {
		t.Fatalf("ambiguous flags accepted: %d", c)
	}
}
