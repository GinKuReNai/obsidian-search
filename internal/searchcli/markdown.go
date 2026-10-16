package searchcli

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type Section struct {
	Heading string `json:"heading"`
	Start   int    `json:"start_line"`
	End     int    `json:"end_line"`
	Body    string `json:"-"`
}

func linesOf(b []byte) []string {
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
}

// Parse uses CommonMark's block parser; source line positions remain unchanged.
func Parse(b []byte) ([]Section, error) {
	if !utf8.Valid(b) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	ls := linesOf(b)
	source := []byte(strings.Join(ls, "\n"))
	if len(ls) > 0 && ls[0] == "---" {
		for i := 1; i < len(ls); i++ {
			if ls[i] == "---" || ls[i] == "..." {
				masked := append([]string(nil), ls...)
				for j := 0; j <= i; j++ {
					masked[j] = ""
				}
				source = []byte(strings.Join(masked, "\n"))
				break
			}
		}
	}
	type mark struct {
		line, level int
		title       string
	}
	lineStarts := []int{0}
	for i, b := range source {
		if b == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	marks := []mark{}
	root := goldmark.DefaultParser().Parse(text.NewReader(source))
	err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if h, ok := n.(*ast.Heading); ok && h.Lines().Len() > 0 {
				seg := h.Lines().At(0)
				line := sort.Search(len(lineStarts), func(i int) bool { return lineStarts[i] > seg.Start })
				marks = append(marks, mark{line, h.Level, string(h.Text(source))})
			}
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk Markdown: %w", err)
	}
	result := []Section{}
	add := func(heading string, start, end int) {
		if start > end {
			return
		}
		for a := start; a <= end; a += 120 {
			z := min(a+119, end)
			body := strings.Join(ls[a-1:z], "\n")
			if strings.TrimSpace(body) != "" {
				result = append(result, Section{heading, a, z, body})
			}
		}
	}
	first := len(ls) + 1
	if len(marks) > 0 {
		first = marks[0].line
	}
	// Frontmatter is metadata, not a searchable preamble.
	pre := 1
	if len(ls) > 0 && ls[0] == "---" {
		for i := 1; i < len(ls); i++ {
			if ls[i] == "---" || ls[i] == "..." {
				pre = i + 2
				break
			}
		}
	}
	add("", pre, first-1)
	stack := []mark{}
	for i, m := range marks {
		for len(stack) > 0 && stack[len(stack)-1].level >= m.level {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, m)
		titles := []string{}
		for _, s := range stack {
			titles = append(titles, s.title)
		}
		end := len(ls)
		if i+1 < len(marks) {
			end = marks[i+1].line - 1
		}
		add(strings.Join(titles, " / "), m.line, end)
	}
	return result, nil
}
