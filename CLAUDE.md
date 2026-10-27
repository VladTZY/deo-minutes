# DEO Minute

Portable Windows app (Go + Wails v2) that joins a Webex meeting as a guest bot, transcribes it with Amazon Transcribe and fills the company's project-workbook Excel template via the Bedrock gateway. `PLAN.md` is the source of truth for scope, decisions, task IDs and open questions: read it before starting any task, tick tasks and append to its Decision log when done.

## Toolchain (macOS dev box)

Go lives in `~/sdk/go` (Homebrew cannot build here). Prefix shell commands with
`export PATH=$HOME/sdk/go/bin:$HOME/go/bin:$PATH`.

Gate before every commit, all must pass:
- `go vet ./...` and `golangci-lint run ./...` and `GOOS=windows golangci-lint run ./...`
- `go test ./...`
- `wails build -platform windows/amd64 -trimpath -ldflags "-s -w -X main.version=<v>"` → `build/bin/DEOMinute.exe`

## Modes

DEMO (default) = local whisper.cpp + Romanian model, then OpenRouter, step by step. PROD = Amazon Transcribe live + Bedrock, automatic. See PLAN.md §2.1. The local transcriber is not in git: `scripts/prepare-local-stt.sh` builds `build/dist/{whisper,models}` (needs `HF_TOKEN` for the gated Romanian model, or `MODEL=stock`), and `scripts/build-windows.sh` packs it into the zip. Real-whisper test: `DEO_WHISPER_EXE=… DEO_WHISPER_MODEL=… DEO_TEST_PCM=… go test -run TestLocalWhisper ./internal/stt`.

## Rules that are easy to break

- **No admin rights, no cgo.** Pure Go only; write only under `config.AppDataDir()` and the output folder. The manifest stays `asInvoker`.
- **No GPU** (target laptops forbid it): WebView2 and the bot browser always run with `--disable-gpu --disable-gpu-compositing`; keep CSS free of blur/backdrop-filter/3D.
- **Romanian user text** goes through `internal/i18n/ro.json` (`i18n.T`, `i18n.Errorf`; frontend `t()`); `TestAllReferencedKeysExist` fails on a missing key. Excel headers and enum values stay English (template).
- **Secrets** only via `internal/secrets` (Credential Manager). Log with `slog`; `logx` redacts, but keep secrets and transcript text out of log calls anyway.
- **Excel output** starts from `templates/project_workbook_template.xlsx`; write values into table rows only, never restyle or restructure the template.
- **Webex selectors** live only in `internal/bot/selectors.go`.
- Commits: conventional commits, no AI attribution lines.
