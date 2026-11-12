# Engineering standards

Use the Go CLI and server guides for fixed layers, downward dependencies, thin entry points and per-command `init()` registration. The table below records platform choices without waiving those structural rules.

| Source | Pinned commit | Local files |
| --- | --- | --- |
| [alswl/guides](https://github.com/alswl/guides) | `e34c0006b2b79aad61dfd1a6e3a881ff7372cac7` | [Go CLI](guides/go-cli-guides.md), [Go service principles](guides/go-server-guides.md), [macOS UI](guides/macos-app-ui-guide.md), [macOS UI 中文](guides/macos-app-ui-guide.zh-CN.md), [information modeling](guides/fe-page-guide-antd.md) |

The copies preserve upstream wording and attribution. Links to editions or demos outside this selection point to that pinned upstream commit. To update, compare the selected files at a new commit, repeat those link adaptations, and review the exceptions below. No standalone license file was present in the pinned source.

| Topic | apilot convention |
| --- | --- |
| Package layout | Application code uses the fixed `pkg/config`, `pkg/server`, `pkg/services`, `pkg/managers`, `pkg/dal`, `pkg/integrations`, `pkg/common` layers. Only MyGo UI and its private presentation implementation remain in `internal/`. |
| Entry points | Both `main.go` files only call `os.Exit(Execute())`. Each CLI command/subcommand has its own file under `cmd/apilot/` and registers with its parent in `init()`. |
| Business rules | Commands call services; server adapters call the Application Service. Services depend on a constructor-injected storage interface; DAL owns SQL. Files are grouped by entity within each layer. |
| UI | MyGo rendering and platform callbacks stay in UI; drafts/state projections must not depend on MyGo. |
| Storage/configuration | Existing SQLite migrations, macOS data paths and environment patches; no ORM, HTTP server or Viper migration. |
| Tests/checks | Standard Go tests, race, formatting and vet. No additional assertion/lint dependency is required. |
| Version | `pkg/common.Version` and `packaging/mygo.json` must agree; packaging verifies actual binaries. |
| Automation | Existing noninteractive CLI contract takes precedence over generic confirmation examples. |

Only the domain modeling principles from the information-page guide apply; Ant Design/Web/TUI stacks and demos are outside this project.

## Shared Makefile toolkit

`hack/makefile-go/` is an unchanged copy of [alswl/makefile-go](https://github.com/alswl/makefile-go) at `79641c287e33c87873c839145763f74d262dd5e1` (no standalone license file in that source). `hack/build.mk` delegates build/download/clean to its build module; the root Makefile uses its help module. Project variables and desktop packaging remain in the project adapter. The upstream mutating lint, test layout, install, version bump, generation and container targets are not exposed as project commands. Compare the full copied directory when updating; keep project adaptations outside it.
