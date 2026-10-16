package searchcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name, body   string
		heads        []string
		starts, ends []int
	}{
		{"nested", "# 日報\n\n## AI調査\n日本語\n## 開発\nEnglish", []string{"日報", "日報 / AI調査", "日報 / 開発"}, []int{1, 3, 5}, []int{2, 4, 6}},
		{"plain", "hello\nworld", []string{""}, []int{1}, []int{2}},
		{"fenced", "# Real\n```go\n# Fake\n```\ntext", []string{"Real"}, []int{1}, []int{5}},
		{"frontmatter", "---\ntitle: secret\n---\n# 本文\r\n内容\r\n", []string{"本文"}, []int{4}, []int{5}},
		{"setext", "Title\n=====\ntext", []string{"Title"}, []int{1}, []int{3}},
		{"indented", "    # not heading\n", []string{""}, []int{1}, []int{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secs, err := Parse([]byte(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if len(secs) != len(tt.heads) {
				t.Fatalf("%+v", secs)
			}
			for i, s := range secs {
				if s.Heading != tt.heads[i] || s.Start != tt.starts[i] || s.End != tt.ends[i] {
					t.Fatalf("%+v", s)
				}
			}
		})
	}
	if _, err := Parse([]byte{255}); err == nil {
		t.Fatal("invalid UTF8 accepted")
	}
}
func fixture(t *testing.T) (*Store, string) {
	t.Helper()
	base := t.TempDir()
	vault := filepath.Join(base, "vault")
	if err := os.Mkdir(vault, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), vault, filepath.Join(base, "db", "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	return s, vault
}
func put(t *testing.T, vault, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(vault, name), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestIndexLifecycle(t *testing.T) {
	s, v := fixture(t)
	ctx := context.Background()
	put(t, v, "a.md", "# 開発生産性\nAI導入 English meeting [[Wiki]]")
	check := func(want IndexStats, rebuild bool) {
		t.Helper()
		got, e := s.Index(ctx, rebuild)
		if e != nil || got != want {
			t.Fatalf("%+v %v want %+v", got, e, want)
		}
	}
	check(IndexStats{Added: 1}, false)
	check(IndexStats{Unchanged: 1}, false)
	put(t, v, "b.md", "# Other\nmore")
	check(IndexStats{Added: 1, Unchanged: 1}, false)
	put(t, v, "a.md", "# Changed\nupdated")
	check(IndexStats{Updated: 1, Unchanged: 1}, false)
	if e := os.Rename(filepath.Join(v, "a.md"), filepath.Join(v, "renamed.md")); e != nil {
		t.Fatal(e)
	}
	check(IndexStats{Added: 1, Deleted: 1, Unchanged: 1}, false)
	if e := os.Remove(filepath.Join(v, "b.md")); e != nil {
		t.Fatal(e)
	}
	check(IndexStats{Deleted: 1, Unchanged: 1}, false)
	check(IndexStats{Updated: 1}, true)
	put(t, v, "renamed.md", "# NEW\nchanged")
	put(t, v, "z.md", string([]byte{255}))
	if _, e := s.Index(ctx, false); e == nil {
		t.Fatal("expected rollback")
	}
	r, e := s.Search(ctx, "updated", "", 5)
	if e != nil || len(r) != 1 {
		t.Fatalf("rollback lost old state: %+v %v", r, e)
	}
	var count int
	if e = s.DB.QueryRow("SELECT count(*) FROM fts").Scan(&count); e != nil || count != 1 {
		t.Fatalf("FTS inconsistency %d %v", count, e)
	}
}
func TestSearch(t *testing.T) {
	s, v := fixture(t)
	ctx := context.Background()
	put(t, v, "a.md", "# 開発生産性\nAI導入 English meeting C++ a\"b AND OR 日本語 [[Wiki]]\n")
	put(t, v, "b.md", "# Other\nEnglish irrelevant long text "+strings.Repeat("filler ", 100))
	if _, e := s.Index(ctx, false); e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct {
		q string
		n int
	}{{"開発生産性", 1}, {"English", 2}, {"AI", 1}, {"開発", 1}, {"AI English", 1}, {"日本語 English", 1}, {"C++", 1}, {`a"b`, 1}, {"AND OR", 1}, {"absent", 0}, {"%_", 0}, {"Wiki", 1}} {
		t.Run(tt.q, func(t *testing.T) {
			r, e := s.Search(ctx, tt.q, "", 5)
			if e != nil || len(r) != tt.n {
				t.Fatalf("%+v %v", r, e)
			}
		})
	}
	r, e := s.Search(ctx, "English", "", 1)
	if e != nil || len(r) != 1 || r[0].Path != "a.md" {
		t.Fatalf("rank/limit %+v %v", r, e)
	}
	r, e = s.Search(ctx, "English", "b", 5)
	if e != nil || len(r) != 1 || r[0].Path != "b.md" {
		t.Fatalf("path %+v %v", r, e)
	}
	if _, e = s.Search(ctx, "English", "", 51); e == nil {
		t.Fatal("limit accepted")
	}
	if r, e = s.Search(ctx, "AI", "", 5); e != nil || r[0].Score != 0 {
		t.Fatal("short fallback")
	}
}
func TestCLI(t *testing.T) {
	s, v := fixture(t)
	put(t, v, "a.md", "# 見出し\nhello world\nsecond")
	outside := filepath.Join(t.TempDir(), "secret.md")
	put(t, filepath.Dir(outside), "secret.md", "secret")
	if e := os.Symlink(outside, filepath.Join(v, "link.md")); e != nil {
		t.Fatal(e)
	}
	call := func(args ...string) (int, string, string) {
		t.Helper()
		var out, err bytes.Buffer
		args = append(args, "--vault", v, "--db", s.Path)
		code := Run(context.Background(), args, &out, &err)
		return code, out.String(), err.String()
	}
	if c, o, e := call("index", "--format", "json"); c != 0 || !json.Valid([]byte(o)) {
		t.Fatalf("%d %s %s", c, o, e)
	}
	if c, o, e := call("search", "hello", "--format", "json"); c != 0 || !json.Valid([]byte(o)) || !strings.Contains(e, "last index") {
		t.Fatalf("%d %s %s", c, o, e)
	}
	if c, o, _ := call("search", "missing", "--format", "json"); c != 0 || strings.TrimSpace(o) != `{"results":[]}` {
		t.Fatalf("%d %s", c, o)
	}
	if c, o, e := call("show", "a.md", "--section", "見出し", "--format", "json"); c != 0 || !json.Valid([]byte(o)) {
		t.Fatalf("%d %s %s", c, o, e)
	}
	for _, args := range [][]string{{"show", "../secret.md"}, {"show", "link.md"}, {"show", "missing.md"}, {"show", "a.md", "--start-line", "0"}, {"search", "hello", "--limit", "51"}, {"index", "--unknown"}, {"search"}, {"status", "extra"}} {
		if c, o, e := call(args...); c == 0 || o != "" || e == "" {
			t.Fatalf("%v: %d %s %s", args, c, o, e)
		}
	}
	if c, o, e := call("status", "--format", "json"); c != 0 || !json.Valid([]byte(o)) {
		t.Fatalf("%d %s %s", c, o, e)
	}
	var out, err bytes.Buffer
	if c := Run(context.Background(), []string{"index", "--vault", "/nonexistent"}, &out, &err); c != 1 || out.Len() != 0 {
		t.Fatal(c)
	}
}
func TestFeatures(t *testing.T) {
	s, _ := fixture(t)
	var version string
	if e := s.DB.QueryRow("SELECT sqlite_version()").Scan(&version); e != nil {
		t.Fatal(e)
	}
	t.Log("SQLite", version)
	if _, e := s.DB.Exec("INSERT INTO fts(heading,body) VALUES('test','日本語 substring')"); e != nil {
		t.Fatal(e)
	}
	var n int
	if e := s.DB.QueryRow(`SELECT count(*) FROM fts WHERE fts MATCH '"日本語"'`).Scan(&n); e != nil || n != 1 {
		t.Fatalf("FTS5/trigram %d %v", n, e)
	}
}

func TestIndexCancellationAndIgnoredDirectories(t *testing.T) {
	s, v := fixture(t)
	ctx := context.Background()
	put(t, v, "a.md", "# Test\noriginal")
	if e := os.Mkdir(filepath.Join(v, ".obsidian"), 0700); e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(v, ".obsidian"), "ignored.md", "secret")
	if _, e := s.Index(ctx, false); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(v, "a.md"))
	if e != nil {
		t.Fatal(e)
	}
	put(t, v, "a.md", "# Test\nmodified")
	if e = os.Chtimes(filepath.Join(v, "a.md"), info.ModTime(), info.ModTime()); e != nil {
		t.Fatal(e)
	}
	stats, e := s.Index(ctx, false)
	if e != nil || stats.Updated != 1 || stats.Unchanged != 0 {
		t.Fatalf("hash detection %+v %v", stats, e)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = s.Index(cancelCtx, false); e == nil {
		t.Fatal("cancellation ignored")
	}
	r, e := s.Search(ctx, "secret", "", 5)
	if e != nil || len(r) != 0 {
		t.Fatalf("ignored directory %+v %v", r, e)
	}
}
func TestChunksAndDuplicates(t *testing.T) {
	body := "# Same\n" + strings.Repeat("line\n", 130) + "# Same\nend\n"
	secs, e := Parse([]byte(body))
	if e != nil || len(secs) != 3 {
		t.Fatalf("%+v %v", secs, e)
	}
	if secs[0].Start != 1 || secs[0].End != 120 || secs[1].Start != 121 || secs[1].End != 131 || secs[2].Start != 132 {
		t.Fatalf("%+v", secs)
	}
}
func TestDatabaseAliasRejected(t *testing.T) {
	s, v := fixture(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if e := os.Symlink(v, alias); e != nil {
		t.Fatal(e)
	}
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"index", "--vault", v, "--db", filepath.Join(alias, "note.md")}, &out, &stderr); code != 1 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	if _, e := os.Stat(filepath.Join(v, "note.md")); !os.IsNotExist(e) {
		t.Fatal("vault written")
	}
	_ = s
}

func TestConcurrentIndex(t *testing.T) {
	s, v := fixture(t)
	put(t, v, "a.md", "# Concurrent\n日本語")
	other, e := Open(context.Background(), v, s.Path)
	if e != nil {
		t.Fatal(e)
	}
	defer other.DB.Close()
	done := make(chan error, 2)
	for _, store := range []*Store{s, other} {
		go func(st *Store) { _, err := st.Index(context.Background(), false); done <- err }(store)
	}
	for i := 0; i < 2; i++ {
		if e = <-done; e != nil {
			t.Fatal(e)
		}
	}
	var count int
	if e = s.DB.QueryRow("SELECT count(*) FROM fts").Scan(&count); e != nil || count != 1 {
		t.Fatalf("%d %v", count, e)
	}
}
