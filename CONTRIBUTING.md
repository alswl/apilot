# Contributing

Read the [engineering standards](docs/standards/README.md), [domain language](GLOSSARY.md) and [architecture](docs/architecture.md) before changing behavior.

Development requires macOS, Xcode Command Line Tools, Python 3, and the Go toolchain pinned in `go.mod`. Toolchain and module downloads require network access.

Use `make help` for available commands. `make check` is read-only, `make test-core` validates logic without a desktop session, and `make race` checks the complete suite. `make app` builds an ad-hoc signed App without release credentials. Packaged UI and lifecycle checks require a desktop session and disposable data.

Keep CLI JSON, exit codes, saved data and lifecycle behavior compatible. Include the checks actually performed and any unverified platform behavior with your change. Public documentation describes current behavior; keep logs, screenshots and one-off verification records out of the documentation tree.
