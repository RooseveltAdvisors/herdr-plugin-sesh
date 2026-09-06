# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go CLI plugin for Herdr named `herdr-sesh`.

- `cmd/herdr-sesh/` contains the executable entry point.
- `internal/app/` owns CLI routing and command behavior.
- `internal/config/`, `internal/model/`, `internal/sources/`, `internal/state/`, and related packages hold domain logic.
- `docs/` contains user-facing configuration and keybinding notes.
- `testdata/` stores fixture TOML used by tests and smoke checks.
- `herdr-plugin.toml` is the plugin manifest and release version source.
- `bin/` is build output; do not treat generated binaries as source.

## Build, Test, and Development Commands

- `mise install` installs pinned tools from `mise.toml`: Go, `cargo-binstall`, `golangci-lint`, `gotestsum`, `just`, `fzf`, `bat`, `eza`, `git-cliff`, and `prek`.
- `just` lists available development recipes from the `justfile`.
- `just fmt-check` checks formatting; `just fmt` applies it.
- `just lint` runs `golangci-lint run ./...`.
- `just test` runs the race-enabled test suite through `gotestsum`.
- `just build` builds the local plugin binary at `bin/herdr-sesh`.
- `just run` runs `./cmd/herdr-sesh` through `go run`.
- `./bin/herdr-sesh --version` smoke-tests the built CLI.
- `./bin/herdr-sesh list --json --config testdata/sesh.toml` checks fixture-backed session listing.

CI runs formatting, vet, tests, build, and CLI smoke checks; mirror those checks before opening a pull request. Run all local checks through `just` recipes - bare `go test`/`go build`/`gofmt` invocations run outside the pinned toolchain (missing golangci-lint and race detection) and do not count as validation.

Never declare work done or open a PR on bare `gofmt`/`go test`/`go build` output alone. The canonical local gate is `just check` (lint, formatting, race-enabled tests,
and release-ref validation), followed by `just build`, the version smoke check,
and the fixture-backed `list --json` smoke check. If `mise`/`prek` are unavailable, run `mise install` first and rerun the gate instead of approving checks that could not run.

## Coding Style & Naming Conventions

Use idiomatic Go formatting and short, lowercase package names. Keep command orchestration in `internal/app` and reusable logic in narrow `internal/*` packages. Prefer table tests only when they reduce repetition. Use `xh` instead of `curl` and `uv` when Python is needed.

Tool versions belong in `mise.toml`; update that file when adding or changing shared developer tooling.

## Testing Guidelines

Tests use Go's standard `testing` package and live beside the code as `*_test.go`. Name tests as `Test<Behavior>` and keep file fixtures in `testdata/`. New behavior should include the smallest focused regression test.

## Commit & Pull Request Guidelines

Recent commits use Conventional Commit-style subjects, for example `feat: cache session list when enabled` and `fix: pad native picker top border`. Keep subjects imperative and scoped when useful (`feat:`, `fix:`, `ci:`, `docs:`).

Pull requests should include a short description, linked issue when applicable, and the exact validation commands run. Include CLI output or screenshots only when changing user-visible command behavior.

When syncing `upstream/main`, fetch `origin` and `upstream` first, merge the
upstream default branch into a feature branch, and land that sync PR with a
merge commit; do not squash or rebase it.

## Release & Configuration Notes

Release tags must start with `v` and match `version` in `herdr-plugin.toml`. Configuration lookup order is documented in `README.md`; preserve compatibility with Sesh-style TOML and existing `testdata/sesh.toml` fixtures.

`last` and `last-agent` are strict two-target toggles. Authoritative focus observation comes from plugin event hooks (`workspace.focused` / `tab.focused`) in `herdr-plugin.toml` — prefer `HERDR_PLUGIN_EVENT_JSON` over ambient `HERDR_*` for the focused target. Pair state lives in `internal/state` (`FocusMRU`) under a per-`HERDR_SESSION` subdirectory of `HERDR_PLUGIN_STATE_DIR` (default session keeps the root). Do not replace this with an N-item history cycle. Agent/tab and workspace pairs must stay orthogonal; cross-workspace `last-agent` jumps use `PrepareAgentJump` so they do not rewrite workspace state. Closed or missing targets clear only the saved slot and refuse, rather than falling back to older picker history.

Keep all Herdr CLI calls behind `internal/herdr.NewCLIClient`. Every consumer of
`HERDR_BIN_PATH` must require a regular executable (`-x`) and fall back to the
PATH-resolved `herdr` (`exec.LookPath`, equivalent to `command -v`) before
invocation; the stale-path regressions live in `internal/herdr/client_test.go`.
Missing-target cleanup must use `IsMissingTarget`'s narrow stderr classification,
not generic command or shell errors.

## Backpass
Backpass trains AGENTS.md from landed sessions; config in `.backpassrc.json` is gpu-specific (cloneRoots point at this machine's pool slots). Run `bunx backpass@latest scan --force --json` then `bunx backpass@latest`.

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.
