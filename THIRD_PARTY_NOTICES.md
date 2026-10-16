# Third-party notices

This project's source code is licensed under MIT. Dependencies retain their own licenses:

| Direct dependency | License | Source |
| --- | --- | --- |
| Goldmark | MIT | https://github.com/yuin/goldmark/blob/master/LICENSE |
| modernc.org/sqlite | BSD-3-Clause | https://gitlab.com/cznic/sqlite/-/blob/master/LICENSE |

SQLite itself is in the public domain: https://www.sqlite.org/copyright.html.

Pinned dependency versions, including transitive modules, are recorded in go.mod and go.sum. When redistributing compiled binaries, preserve the applicable dependency license notices. Build products are not checked into this source repository.

## Indirect dependencies declared in go.mod

These licenses were checked against the pinned module sources:

| Module | Version | License |
| --- | --- | --- |
| github.com/dustin/go-humanize | v1.0.1 | MIT |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause |
| github.com/mattn/go-isatty | v0.0.24 | MIT |
| github.com/ncruces/go-strftime | v1.0.0 | MIT |
| github.com/remyoudompheng/bigfft | v0.0.0-20230129092748-24d4a6f8daec | BSD-3-Clause |
| golang.org/x/sys | v0.48.0 | BSD-3-Clause |
| modernc.org/libc | v1.77.1 | BSD-3-Clause |
| modernc.org/mathutil | v1.7.1 | BSD-3-Clause |
| modernc.org/memory | v1.12.1 | BSD-3-Clause |

The SQLite driver also carries [upstream third-party notices for v1.60.1](https://gitlab.com/cznic/sqlite/-/blob/v1.60.1/LICENSE-3RD-PARTY.md), including notices inherited from libc (such as musl's MIT license). Preserve the relevant upstream notices when distributing binaries. Optional driver extensions and dependency build tools have their own notices; they are not all linked into this CLI.
