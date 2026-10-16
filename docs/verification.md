# Verification

## Publication review (2026-10-16)

Environment: macOS arm64. The minimum Go version is 1.27.2.

Local checks passed with Go 1.27.2: formatting, module checksums, normal and CGO-free builds, vet, tests, the race detector, and govulncheck (no vulnerabilities found). Tests use real SQLite databases and include regression coverage for hard-linked database paths and named pipes. See the GitHub Actions checks for results on Linux and both macOS architectures.

The initial vulnerability scan on Go 1.27.0 reported GO-2026-6604 in the standard library (Windows). The project minimum was raised to Go 1.27.2, which contains the fix. The vulnerability tool must run with the project's Go toolchain; CI sets `GOTOOLCHAIN=local` after setup-go to avoid selecting an older toolchain for the tool itself.

The included sample vault was exercised through index, incremental index, Japanese and mixed-term search, show, and status. Every command returned valid JSON, search returned the expected note, and the source file list and SHA-256 hashes remained unchanged. Generated indexes were placed in a temporary directory outside the vault.

The source publication excludes binaries, databases, archives, environment files, and local agent settings. The MIT license and direct and declared indirect dependency licenses were checked against downloaded module sources. Local README links were checked.

## Scope

Tests cover FTS5/trigram matching, literal queries, ranking, limits, path filters, JSON and exit codes; incremental updates, deletion, rename, cancellation and rollback; concurrent indexing and vault binding; CommonMark headings, frontmatter, CRLF and chunking; path traversal, symlink escapes, database aliases and special files.

Search reflects the last successful index. The vault is not an operating-system snapshot, and concurrent edits require reindexing. Windows execution is outside the supported CI matrix.
