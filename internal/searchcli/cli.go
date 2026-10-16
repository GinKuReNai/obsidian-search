package searchcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Run(ctx context.Context, args []string, out, stderr io.Writer) int {
	err := run(ctx, args, out, stderr)
	if err == nil {
		return 0
	}
	fmt.Fprintln(stderr, "obsidian-search:", err)
	var usage *usageError
	if errors.As(err, &usage) {
		return 2
	}
	return 1
}

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }
func run(ctx context.Context, args []string, out, stderr io.Writer) error {
	if len(args) == 0 {
		return &usageError{"usage: obsidian-search {index|search|show|status} [options]"}
	}
	cmd := args[0]
	if cmd == "--help" || cmd == "help" {
		fmt.Fprintln(out, "obsidian-search {index|search|show|status} --vault PATH [--db PATH] [--format json|text]\nsearch QUERY [--limit 5] [--path PREFIX]\nshow PATH [--start-line N --end-line N | --section HEADING]\nindex [--rebuild]")
		return nil
	}
	if cmd != "index" && cmd != "search" && cmd != "show" && cmd != "status" {
		return &usageError{"unknown command: " + cmd}
	}
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	f.SetOutput(stderr)
	vault := f.String("vault", os.Getenv("OBSIDIAN_VAULT"), "vault directory (or OBSIDIAN_VAULT)")
	dbpath := f.String("db", os.Getenv("OBSIDIAN_SEARCH_DB"), "database location")
	format := f.String("format", "text", "json or text")
	var limit, start, end int
	var prefix, section string
	var rebuild bool
	switch cmd {
	case "index":
		f.BoolVar(&rebuild, "rebuild", false, "reparse every file atomically")
	case "search":
		f.IntVar(&limit, "limit", 5, "results, 1–50")
		f.StringVar(&prefix, "path", "", "literal path prefix")
	case "show":
		f.IntVar(&start, "start-line", 1, "first line")
		f.IntVar(&end, "end-line", 0, "last line; default 120 lines")
		f.StringVar(&section, "section", "", "exact hierarchical heading; rejects ambiguity")
	}
	// Standard flag parsing with flags accepted before or after a positional argument.
	opts := []string{}
	pos := []string{}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") {
			opts = append(opts, a)
			name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
			if !strings.Contains(a, "=") && name != "rebuild" && name != "help" && name != "h" {
				if i+1 >= len(args) {
					return &usageError{"missing value for " + a}
				}
				i++
				opts = append(opts, args[i])
			}
		} else {
			pos = append(pos, a)
		}
	}
	if err := f.Parse(opts); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &usageError{err.Error()}
	}
	expected := 0
	if cmd == "search" || cmd == "show" {
		expected = 1
	}
	if len(pos) != expected {
		return &usageError{fmt.Sprintf("%s requires %d positional argument(s)", cmd, expected)}
	}
	if *format != "json" && *format != "text" {
		return &usageError{"format must be json or text"}
	}
	if cmd == "search" {
		if err := validateQuery(pos[0], limit); err != nil {
			return &usageError{err.Error()}
		}
	}
	if *vault == "" {
		return &usageError{"set --vault or OBSIDIAN_VAULT"}
	}
	if strings.HasPrefix(*vault, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		*vault = filepath.Join(home, (*vault)[2:])
	}
	v, err := filepath.Abs(*vault)
	if err != nil {
		return err
	}
	v, err = filepath.EvalSymlinks(v)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	emit := func(value any) error { return json.NewEncoder(out).Encode(value) }
	if cmd == "show" {
		s := &Store{Vault: v}
		b, err := s.Read(pos[0])
		if err != nil {
			return err
		}
		ls := linesOf(b)
		if section != "" {
			conflict := false
			f.Visit(func(fl *flag.Flag) {
				if fl.Name == "start-line" || fl.Name == "end-line" {
					conflict = true
				}
			})
			if conflict {
				return &usageError{"--section cannot be combined with line range flags"}
			}

			secs, e := Parse(b)
			if e != nil {
				return e
			}
			found := false
			for _, sec := range secs {
				if sec.Heading == section {
					if found {
						return fmt.Errorf("ambiguous or chunked section; use line range")
					}
					start, end, found = sec.Start, sec.End, true
				}
			}
			if !found {
				return fmt.Errorf("section not found")
			}
		}
		if start < 1 || start > len(ls) {
			return &usageError{"start-line must be within file"}
		}
		if end == 0 {
			end = start + min(119, len(ls)-start)
		}
		if start < 1 || end < start || start > len(ls) || end > len(ls) || end-start+1 > 500 {
			return &usageError{"line range must be within file, 1–500 lines"}
		}
		content := strings.Join(ls[start-1:end], "\n")
		if len(content) > 128*1024 {
			return fmt.Errorf("output exceeds 128 KiB; request fewer lines")
		}
		if *format == "json" {
			return emit(struct {
				Path    string `json:"path"`
				Start   int    `json:"start_line"`
				End     int    `json:"end_line"`
				Content string `json:"content"`
			}{pos[0], start, end, content})
		}
		for i := start; i <= end; i++ {
			if _, err = fmt.Fprintf(out, "%d: %s\n", i, ls[i-1]); err != nil {
				return err
			}
		}
		return nil
	}
	if *dbpath == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(v))
		*dbpath = filepath.Join(base, "obsidian-search", hex.EncodeToString(sum[:16]), "index.db")
	}
	*dbpath, err = filepath.Abs(*dbpath)
	if err != nil {
		return err
	}
	// Resolve existing ancestors before enforcing placement (including symlinked parents).
	resolvedDB, e := canonicalDestination(*dbpath)
	if e != nil {
		return e
	}
	*dbpath = resolvedDB
	// Never permit the generated index to overwrite a vault file.
	if rel, e := filepath.Rel(v, *dbpath); e == nil && filepath.IsLocal(rel) {
		return fmt.Errorf("database must be outside vault")
	}
	if cmd != "index" {
		if _, err = os.Stat(*dbpath); err != nil {
			return fmt.Errorf("index unavailable; run index first: %w", err)
		}
	}
	s, err := Open(ctx, v, *dbpath)
	if err != nil {
		return err
	}
	defer s.DB.Close()
	switch cmd {
	case "index":
		stats, e := s.Index(ctx, rebuild)
		if e != nil {
			return e
		}
		if *format == "json" {
			return emit(stats)
		}
		_, err = fmt.Fprintf(out, "added=%d updated=%d deleted=%d unchanged=%d\n", stats.Added, stats.Updated, stats.Deleted, stats.Unchanged)
	case "search":
		var updated string
		if e := s.DB.QueryRowContext(ctx, "SELECT value FROM meta WHERE key='updated'").Scan(&updated); e != nil {
			return fmt.Errorf("index not ready; run index: %w", e)
		}
		fmt.Fprintln(stderr, "Note: results reflect the last index; run index after edits. Use show for current text.")
		results, e := s.Search(ctx, pos[0], prefix, limit)
		if e != nil {
			return e
		}
		if *format == "json" {
			return emit(struct {
				Results []Result `json:"results"`
			}{results})
		}
		for _, r := range results {
			if _, err = fmt.Fprintf(out, "%s:%d-%d  %s\n%s\n", r.Path, r.Start, r.End, r.Heading, r.Snippet); err != nil {
				return err
			}
		}
	case "status":
		var files, sections int
		var updated string
		if err = s.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM files),
            (SELECT count(*) FROM sections),
            COALESCE((SELECT value FROM meta WHERE key='updated'),'')`).Scan(&files, &sections, &updated); err != nil {
			return err
		}
		state := "snapshot"
		if updated == "" {
			state = "not_indexed"
		}
		if *format == "json" {
			return emit(struct {
				Index    string `json:"index"`
				Files    int    `json:"files"`
				Sections int    `json:"sections"`
				Updated  string `json:"updated_at"`
				State    string `json:"state"`
			}{s.Path, files, sections, updated, state})
		}
		_, err = fmt.Fprintf(out, "index: %s\nfiles: %d\nsections: %d\nupdated_at: %s\nstate: %s (filesystem changes not checked)\n", s.Path, files, sections, updated, state)
	}
	return err
}

func canonicalDestination(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolved, err = canonicalDestination(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}
