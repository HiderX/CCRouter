# Verification log / 本机验证记录

Date: 2026-10-29. Apple Silicon MacBook Pro (M1 Pro), macOS 15.7.9, Go 1.27.3, Claude Code 2.1.281.

## Tests

- `make test` (`go test -race ./internal/... ./cmd/...`), `go vet ./...`, `go mod verify`: pass. FSEvents integration tests need permission to watch files and were run outside the sandbox. All other tests use temporary data and stand-in executables.
- Cross-compilation: `GOOS=linux` and `GOOS=windows` builds of `cmd/ccrouter` and the core packages compile and pass `go vet` (with `CGO_ENABLED=0`; a working SQLite build needs cgo). Not run on those systems.
- DarwinKit bridge tests and regression tests for the pinned fixes: pass, including the empty NSArray and 10,000 narrow native return values followed by GC.
- Launcher end to end, with a stand-in `claude` and a temporary `CCROUTER_HOME`: a new conversation bound to A keeps A after the default changes to B; resume and continue use A; resuming on B is refused; an account that never logged in is refused without leaving a binding.
- Interactive launcher in a real pseudo-terminal (`script`): account picker with arrows and digits, Esc to cancel, conversation picker with filtering and a search term, resume from another folder changing into the conversation's folder, `--account` and non-interactive fallbacks, Claude management commands rejected with a hint.
- Quota through Claude Code: `claude -p /usage` (no tools, MCP or settings sources, no saved session) finished in about 2 s with `num_turns: 0`, zero tokens and zero cost, left no process behind, and wrote `cachedUsageUtilization` for the logged-in `accountUuid`. Unit tests cover the cache freshness, run intervals, a cache written for another account, and logged-out accounts.
- First-run setup: before the fix, an account logged in with `claude auth login` opened Claude's onboarding (theme, then login method). After `hasCompletedOnboarding` was set, Claude went straight to the folder trust prompt.
- Native AppKit: 24 real disclosure clicks and 6 close/reopen cycles without a crash; expanded, collapsed and empty panels exported; light and dark appearances checked offscreen.

## Resource use

Menu bar app, real data (two accounts, a few hundred MB of transcripts), Claude sessions active:

| Metric | Result |
| --- | --- |
| Physical footprint (Activity Monitor) | about 48–49 MB |
| RSS | about 84–86 MB |
| CPU, steady state over 60 s | 0.03 s, about 0.05% |

RSS also counts mapped system frameworks, so the physical footprint is the figure Activity Monitor shows. Across its first 15 minutes, including startup and opening the panel, the app used 6.9 s of CPU.

Launcher overhead, median of 30 runs of `ccrouter run` against a stand-in `claude` that exits at once: about 18 ms more than starting the stand-in directly. It opens the database, writes the binding and checks the Keychain entry, then `exec`s.

Isolated demo (`dist/ccrouter demo --smoke dist/ui-smoke`), panel closed, measured 10 s after a 3 s pause: idle CPU about 0.004–0.015% in quiet runs, RSS about 80–84 MB, Go heap about 2–3 MB, 3 goroutines. Some runs read 1.6–8.7% with no code change. Sampling those idle windows with `sample` found every thread waiting, so the likely cause is desktop activity at the time, such as menu bar relayout when another status item appears. It has not been confirmed. Raising the redraw count to 240 and 480 gave 86.2 and 86.6 MB RSS, which levels off, so there is no per-redraw leak.

## Not yet tested

- Liquid Glass on macOS 26; this machine runs macOS 15.
- Linux and Windows on real systems, and long-term runs on very large histories.

`designs/native-*-preview.png` are offscreen renders of the real AppKit views with demo data. The popover material is not captured offscreen, so a window background color stands in for it.
