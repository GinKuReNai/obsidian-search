# Contributing

Bug reports and focused pull requests are welcome. For larger changes, open an issue first so the scope can be discussed.

## Before submitting

1. Describe the observed and expected behavior. Include a small synthetic Markdown example when useful; do not upload private vaults or index databases.
2. Keep changes focused. Prefer concrete types, the standard library, and tests with real SQLite databases.
3. Add a regression test for behavioral fixes and update both README languages when user-facing behavior changes.
4. Run the following with Go 1.27.2 or later:

```sh
gofmt -w cmd internal
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/obsidian-search
```

The project prioritizes source data preservation, small agent context, correct search, and maintainability. Embeddings, servers, cloud sync, and daemon mode are outside the current scope.

By contributing, you agree that your contributions are licensed under the project's MIT license.
