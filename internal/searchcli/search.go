package searchcli

import (
	"context"
	"fmt"
	"strings"
)

type Result struct {
	Path    string  `json:"path"`
	Heading string  `json:"heading"`
	Start   int     `json:"start_line"`
	End     int     `json:"end_line"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

func excerpt(body string, terms []string) string {
	r := []rune(body)
	at := 0
	lower := strings.ToLower(body)
	for _, t := range terms {
		if i := strings.Index(lower, strings.ToLower(t)); i >= 0 {
			at = len([]rune(lower[:i]))
			break
		}
	}
	start := max(0, at-50)
	end := min(len(r), start+200)
	return string(r[start:end])
}
func (s *Store) Search(ctx context.Context, q, prefix string, limit int) ([]Result, error) {
	if err := validateQuery(q, limit); err != nil {
		return nil, err
	}
	terms := strings.Fields(q)
	match := []string{}
	args := []any{}
	where := []string{}
	for _, t := range terms {
		if len([]rune(t)) >= 3 {
			match = append(match, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
		} else {
			where = append(where, "instr(lower(s.heading || char(10) || s.body),lower(?))>0")
			args = append(args, t)
		}
	}
	from := "sections s"
	score := "0.0"
	if len(match) > 0 {
		from += " JOIN fts ON fts.rowid=s.id"
		score = "bm25(fts,3.0,1.0)"
		where = append(where, "fts MATCH ?")
		args = append(args, strings.Join(match, " AND "))
	}
	where = append(where, "substr(s.path,1,length(?))=?")
	args = append(args, prefix, prefix, limit)
	rows, err := s.DB.QueryContext(ctx, "SELECT s.path,s.heading,s.start,s.end,s.body,"+score+" FROM "+from+" WHERE "+strings.Join(where, " AND ")+" ORDER BY "+score+",s.path,s.start LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()
	out := []Result{}
	for rows.Next() {
		var r Result
		var body string
		if err = rows.Scan(&r.Path, &r.Heading, &r.Start, &r.End, &body, &r.Score); err != nil {
			return nil, err
		}
		r.Snippet = excerpt(body, terms)
		out = append(out, r)
	}
	return out, rows.Err()
}

func validateQuery(q string, limit int) error {
	terms := strings.Fields(q)
	if len(terms) == 0 || len(terms) > 32 || len(q) > 4096 {
		return fmt.Errorf("query must contain 1–32 terms and at most 4096 bytes")
	}
	if limit < 1 || limit > 50 {
		return fmt.Errorf("limit must be 1–50")
	}
	return nil
}
