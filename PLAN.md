# DEO Minutes: implementation plan

> Audience: a coding agent (Claude) executing this plan. Every task has an ID, the files
> it touches, and a **Done when** line. Work top to bottom. Tick `[x]` when done and
> record decisions in the "Decision log" at the bottom. Do not skip M0: it de-risks the
> assumptions everything else rests on. Tasks marked **BLOCKED(Qn)** wait on an open
> question in section 9; build everything around them against fakes.

---

## 0. Product summary

A portable Windows desktop app. The user pastes a Webex meeting link, fills in the
project name and meeting number, and presses **Pornește**. A bot joins the meeting as a
guest, captures the meeting audio and who is speaking, transcribes it live with Amazon
Transcribe (Romanian, with English terms), sends the speaker-labelled transcript to an
LLM through the company's AWS Bedrock gateway, and fills the company's **Project
workbook** Excel template (Meetings, Action Item Register, Interdependencies, Risk
Register, Decision Log, Change Log). The `.xlsx` is saved in the user's **Documents**
folder. App UI and all generated content are in Romanian.

Sizing target: meetings up to **4 hours**, up to **50 participants**, Romanian mixed
with English, speaker attribution required.

## 1. Hard constraints (never violate)

| ID | Constraint | Consequence for implementation |
|----|------------|--------------------------------|
| C1 | Runs on Windows 10/11 **without admin rights** | Single portable `.exe`. No installer, no service, no driver, no virtual audio cable, no writes outside `%LOCALAPPDATA%` and the Documents output folder, no ports < 1024, no registry writes outside `HKCU`. |
| C2 | Written in **Go** (chosen over Rust, see D1) | Pure Go. No cgo, so we can cross-compile from macOS. |
| C3 | Romanian everywhere the user reads | UI strings via `i18n/ro.json`; errors in Romanian; workbook **content** (descriptions, rationale, notes) in Romanian. Exception (Q12): the template's headers, sheet names and enum values stay **English**. |
| C4 | AI goes through the **AWS Bedrock gateway** | One `llm.Client` interface; gateway URL + model are config, not code. |
| C5 | Secrets never in code, logs, or plain files | Gateway key and AWS credentials stored in Windows Credential Manager (per-user, no admin). Logs redact them. |
| C6 | Meeting data is personal data (GDPR) | User informs participants verbally (Q9). Bot display name still says it records. Transcripts/audio stay local except the Transcribe stream and the LLM call; retention setting. |
| C7 | **No GPU acceleration anywhere** (target laptops do not permit it) | Wails/WebView2: `windows.Options{WebviewGpuIsDisabled: true}` **and** set `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--disable-gpu --disable-gpu-compositing` in-process before `wails.Run`. Bot browser: `--disable-gpu --disable-gpu-compositing` (keep software rendering on; never pass `--disable-software-rasterizer`). No local GPU ML. Frontend: no CSS `backdrop-filter`, `filter: blur`, 3D transforms, or heavy animations. |
| C8 | The output must be the **company template**, not a new layout | Start from `templates/project_workbook_template.xlsx` (copy of `260407_Project workbook_MASTER.xlsx`), keep its sheets, tables, styles, formulas, comments; only write values into data rows. |

## 2. Key decisions

| ID | Decision | Why | Fallback if it fails |
|----|----------|-----|----------------------|
| D1 | **Go 1.23+** with **Wails v2** for the desktop UI | Single exe, no cgo on Windows (WebView2 is preinstalled on Win10/11), cross-compiles from macOS, mature `chromedp`, official `aws-sdk-go-v2`. Native window, no localhost HTTP port to secure. | Rust + Tauri + `chromiumoxide` + AWS Rust SDK; switch only if Wails blocks us. |
| D2 | **Option A (decided, Q6):** bot = **headless Microsoft Edge driven via CDP (`chromedp`)** joining through the Webex web client as a guest | Easiest and needs the fewest permissions: no Webex admin, no Service App, no Webex secrets. Edge ships with Windows and runs as the user. Meetings are our company's, guests allowed, no password (Q7). | (a) Chrome for Testing in `%LOCALAPPDATA%` if IT blocks Edge remote debugging. (b) Webex Web SDK (section 9.1) if the web UI proves unworkable. (c) Local mode: WASAPI loopback of the user's own Webex client (no admin) while they attend. |
| D3 | Audio capture **inside the page**: hook `RTCPeerConnection` remote audio tracks → WebAudio mix → AudioWorklet → 16 kHz mono PCM → Go via `runtime.AddBinding` | No OS audio devices, no admin, no driver. Captures meeting audio only. | D2(c). |
| D4 | **Transcription: Amazon Transcribe Streaming** (decided, Q3), `ro-RO`, via `aws-sdk-go-v2/service/transcribestreaming` | `ro-RO` is supported for streaming and batch (AWS docs). Runs in the cloud, so no CPU/GPU load on laptops (C7). Live, so the transcript is ready when the meeting ends. | Mixed RO/EN: try `IdentifyMultipleLanguages` with `LanguageOptions=ro-RO,en-US` in S3; if unsupported or worse, plain `ro-RO` (it handles common English terms acceptably). Offline fallback (later, optional): local whisper.cpp CPU build. |
| D5 | **Speaker attribution from Webex, not from voice** | Webex knows who is talking (active-speaker indicator) and who is in the meeting (participants panel). Voice diarization cannot name people and degrades past ~10 speakers; we have 50. | Shared room devices (Q15) are deferred: v1 uses the room's participant name. |
| D6 | LLM call: **Bedrock Converse API with forced tool use** returning JSON for all six record types | Structured, validatable output. | OpenAI-compatible adapter if the gateway is LiteLLM-style. **BLOCKED(Q2)** for the real endpoint; develop against a fake client and, if allowed, direct Bedrock. |
| D7 | Output: **fill the XLSX template with `github.com/xuri/excelize/v2`** (pure Go, maintained) | Keeps the company format (C8). | If excelize damages tables/styles (checked in S4), patch the sheet/table XML inside the zip directly (`archive/zip` + `encoding/xml`). |
| D8 | 4-hour meetings: Transcribe stream rotated every 60 min (seamless reconnect with 2 s overlap); LLM runs map (~30 min windows) → reduce (merge, dedupe, cross-link) | Streaming sessions have a maximum duration; 4 h of Romanian speech is ~60-80k tokens, too much for one reliable extraction pass. | Smaller windows if the gateway's context limit is lower (Q2). |

## 3. Architecture

```
┌──────────────── Wails window (React + TS, Romanian UI) ──────────────────┐
│ Link Webex [__________]  Proiect [_______]  Nr. întâlnire [_]            │
│ Întocmit de [_______]          [Pornește] [Oprește]                      │
│ Stare: În sala de așteptare… / Înregistrare 01:12:31 / Gata ✔            │
└───────────────┬────────────────────────────────────────────▲─────────────┘
                │ Wails bindings (Go methods)                │ events
┌───────────────▼────────────────────────────────────────────┴─────────────┐
│ app.Session (state machine, one meeting at a time per laptop)            │
│  ├─ bot.Joiner       chromedp → msedge.exe --headless (guest join)       │
│  │   ├─ capture.js   RTCPeerConnection hook → PCM chunks   → deoAudio    │
│  │   └─ roster.js    participants + active speaker (500 ms) → deoSpeaker │
│  ├─ stt.Streamer     PCM → Transcribe Streaming (ro-RO) → timed words    │
│  ├─ speakers.Aligner words × active-speaker timeline → labelled lines    │
│  ├─ minutes.Extractor transcript → llm.Client (map/reduce) → Records     │
│  └─ export.Workbook  Records + meeting meta → template .xlsx → Documents  │
│ config (JSON, %LOCALAPPDATA%)  secrets (Credential Manager)  logs        │
└──────────────────────────────────────────────────────────────────────────┘
```

Session states (`internal/app/state.go`):
`Inactiv → Pornire browser → Conectare → Sală de așteptare → Înregistrare → Finalizare transcriere → Generare minută → Salvare Excel → Gata`, plus `Eroare` (Romanian message) from any state. **Oprește** works from `Conectare` onward; the pipeline then continues with what was captured.

Clocks: audio chunks and speaker events are both timestamped in the page with `performance.timeOrigin + performance.now()`, so alignment needs no cross-process clock sync.

## 4. Repository layout (target)

```
DEO_Minutes/
  PLAN.md                       ← this file
  README.md                     ← Romanian, for end users
  CLAUDE.md                     ← agent conventions for this repo (T1.1)
  go.mod  main.go  wails.json
  templates/
    project_workbook_template.xlsx   ← copy of the user's MASTER (go:embed)
  internal/
    app/        session.go state.go bindings.go
    bot/        joiner.go webex_flow.go selectors.go browser.go capture.js roster.js
    stt/        streamer.go transcribe.go rotate.go
    speakers/   timeline.go align.go
    llm/        client.go bedrock.go openai_compat.go fake.go
    minutes/    records.go validate.go extractor.go chunk.go prompts/ro_extract.md prompts/ro_merge.md
    export/     workbook.go tables.go sanitize.go
    config/     config.go paths.go knownfolder_windows.go
    secrets/    keyring.go
    i18n/       i18n.go ro.json
    logx/       log.go redact.go
  frontend/     src/pages/{Main,Setari,Istoric}.tsx
  testdata/     transcript_ro_sample.txt expected_records.json speaker_events.json words.json
  scripts/      build-windows.sh
  .github/workflows/build.yml
```

## 5. Data contracts

### 5.1 Config (`%LOCALAPPDATA%\DEOMinutes\config.json`, no secrets)

```json
{
  "gateway":  { "kind": "bedrock|openai_compat", "base_url": "", "region": "eu-central-1", "model_id": "", "auth": "bearer|sigv4" },
  "transcribe": { "region": "eu-central-1", "language": "ro-RO", "multi_language": ["ro-RO", "en-US"], "rotate_min": 60 },
  "bot":      { "display_name": "Asistent Minute (înregistrează)", "browser_path": "", "lobby_timeout_min": 15, "silence_end_min": 10, "post_chat_notice": false },
  "output":   { "dir": "<Documents>\\DEO Minute", "keep_audio": false, "retention_days": 30 },
  "last":     { "project": "", "prepared_by": "", "meeting_no_by_project": { "<project>": 1 } }
}
```

- `<Documents>` = `windows.KnownFolderPath(windows.FOLDERID_Documents, 0)` (`golang.org/x/sys/windows`), which handles OneDrive-redirected Documents. Never hardcode `%USERPROFILE%\Documents`.
- Secrets in Credential Manager (`github.com/zalando/go-keyring`): `DEOMinutes/gateway` (gateway key) and `DEOMinutes/aws` (access key id + secret, or profile name if SSO). **BLOCKED(Q2, Q3b)** for the exact shape.

### 5.2 Template map (from `260407_Project workbook_MASTER.xlsx`, inspected 2026-10-26)

All six sheets use Excel tables. Row 1 of each table is the header; data rows start
right below. IDs in column `#` are prefilled in the template (and AI-16 is missing in
the master), so **we overwrite the `#` column with our own sequential IDs**.

| Sheet | Table range (template) | Rows available | Columns (exact header text) | Filled by |
|-------|------------------------|---------------|-----------------------------|-----------|
| `Meetings` | `D8:E37` (participants) | 29 | `Participants`, `Local/Call` | Bot roster |
| `Meetings` (cells) | — | — | `B1` project name, `P6` meeting no. (feeds formula in `B6`), `C9` meeting date, `C10` minutes prepared by, `C11` approved by (leave empty), `C12` distributed to (keep "all participants") | UI fields |
| `Action Item Register` | `A5:I44` | 39 | `#` (AI-n), `Title`, `Description`, `Date issued`, `Deadline`, `Team`, `Responsible`, `Status`, `Notes` | LLM |
| `Interdependencies` | `B5:H16` | 11 | `#` (ID-n), `Dependent on (Project name)`, `Dependency`, `Status`, `Action Item(s)`, `Risk(s)`, `Notes` | LLM |
| `Risk Register` | `B6:K16` (group headers in row 5: Identification / Analysis / Response / Tracking) | 10 | `#` (R-n), `Risk title and brief description`, `Cause`, `Severity`, `Probability`, `Decision`, `Risk response actions`, `Owner for implementation`, `Date of update`, `Status update` | LLM |
| `Decision Log` | `B5:I16` | 11 | `#` (D-n), `Statement`, `Date`, `Rationale`, `Made by `, `Agreed with (with the consent of)`, `Status`, `Outcomes` | LLM |
| `Change Log` | `B5:O16` | 11 | `#` (CH-n), `Title`, `Summary description`, `Date submitted`, `Requested by`, `Requestor's priority`, `Change type`, `Cost impact`, `Schedule impact`, `Quality impact`, `Overall impact`, `Priority`, `Status`, `Notes` | LLM |

Controlled values found in the template (cell comments on Change Log headers):
- `Requestor's priority`, `Priority`: `Low` / `Medium` / `High`
- `Change type`: `Small` / `Large` (the template comment spells it "Lanrge"; write `Large`)
- Verified 2026-10-26: the workbook has **no dropdown lists** (its data validations are empty) and no conditional formatting. These cell comments and the sample values below are the only controlled values in the file.
- Change `Status`: `New` / `Approved` / `Escalated` / `Rejected` / `Analyzed` / `Implemented`
- Decision `Status` example: `Active`. `Local/Call` examples: `Call`, `No`.
- Unknown (Q13): Action Item `Status`, Risk `Severity`/`Probability`, Interdependency `Status`, impact columns. Default until answered: `Low/Medium/High` for severity, probability and impacts; `Open` for new action items and interdependencies; `Active` for decisions; `New` for changes. Enum values are English (Q12).

**Capacity:** a 4-hour, 50-person meeting can exceed these row counts (29 participants, 39 actions, 10-11 elsewhere). `export/tables.go` must grow a table: insert rows inside the table range, copy the style of the last data row, and update the table `ref` (and autofilter). Never write below a table without extending it.

### 5.3 Records (LLM tool schema ⇄ Go structs, `minutes/records.go`)

Every record carries evidence; Go assigns IDs and resolves cross-references.

```go
type Evidence struct {
    At      string `json:"marcaj_timp"`  // HH:MM:SS from meeting start
    Speaker string `json:"vorbitor"`     // name from Webex roster
    Quote   string `json:"citat"`        // verbatim, ≤ 200 chars
}
type ActionItem struct {
    Key, Title, Description, Deadline, Responsible, Status string // Key = "a1","a2"… (LLM-local). No Team field: column stays empty (Q14).
    Evidence []Evidence
}
type Decision struct {
    Key, Statement, Rationale, MadeBy, AgreedWith, Status, Outcomes string
    Evidence []Evidence
}
type Risk struct {
    Key, TitleDescription, Cause, Severity, Probability, Decision, ResponseActions, Owner, StatusUpdate string
    Evidence []Evidence
}
type Interdependency struct {
    Key, DependentOnProject, Dependency, Status string
    ActionKeys, RiskKeys []string // resolved to "AI-3, AI-7" / "R-2" on export
    Notes string
    Evidence []Evidence
}
type Change struct {
    Key, Title, Summary, RequestedBy, RequestorPriority, ChangeType,
    CostImpact, ScheduleImpact, QualityImpact, OverallImpact, Priority, Status, Notes string
    Evidence []Evidence
}
```

Field rules (put in the prompt **and** enforced in `validate.go`):
- Free-text fields in Romanian. Names exactly as in the roster. Unknown → empty cell (not "N/A").
- `Deadline` resolved to a date against the meeting date ("vineri" → next Friday); if unresolvable, empty and the phrase goes into `Notes`.
- Enums must match 5.2 exactly; otherwise retry once with the validation error, then blank the field and log.
- Anti-hallucination: each `Evidence.Quote` must fuzzy-match (normalize case/diacritics/whitespace, ≥ 0.85) a transcript line within ±60 s of `At`, said by `Speaker`. A record with no valid evidence is dropped and logged.
- Export: `Notes` (or `Outcomes` / `Status update` where a sheet has no Notes) gets `[01:23:45] Ion Popescu: „citat”` so people can check each row.

Workbook cell writes:
- Dates as real Excel dates (`time.Time`) with the template's number format, not strings.
- Formula-injection guard: any text starting with `= + - @` gets a leading `'`.
- Never touch formula cells (`B6`, `A2`/`B2` on the other sheets).

### 5.4 Output files

Folder `<output.dir>` (default `Documents\DEO Minute`). Per meeting:
- `YYMMDD_<Proiect>_Minuta_<Nr>.xlsx`: filled template (matches the user's `260407_…` naming). One new file per meeting (Q11); never modify an existing MASTER.
- `YYMMDD_<Proiect>_Minuta_<Nr>_transcriere.txt`: `[HH:MM:SS] Nume: text` lines.
- `YYMMDD_<Proiect>_Minuta_<Nr>_meta.json`: start/end, duration, participant count, model ids, app version. No link tokens, no secrets.
- If the file exists, add `_v2`, `_v3`; never overwrite.

## 6. Milestones and tasks

### M0: Spikes (de-risk, throwaway code on branch `spike/*`)

- [ ] **S1 Bot join + audio + speakers on a real locked-down Windows machine.**
  Script in `spikes/join/main.go`: launch `msedge.exe` (resolve `%ProgramFiles(x86)%\Microsoft\Edge\Application\msedge.exe`, then `%ProgramFiles%`) with
  `--headless=new --disable-gpu --disable-gpu-compositing --remote-debugging-port=0 --user-data-dir=%LOCALAPPDATA%\DEOMinutes\browser --use-fake-ui-for-media-stream --use-fake-device-for-media-stream --use-file-for-fake-audio-capture=<silence.wav> --autoplay-policy=no-user-gesture-required --lang=ro`.
  Open the link, "Join from browser", guest name, mute mic/camera, join, wait for admission, inject `capture.js` + `roster.js`, save 5 min of PCM to WAV and the speaker events to JSON.
  **Done when:** a non-admin Windows user gets (1) an audible WAV of other participants, (2) the participant list with names, (3) an active-speaker timeline that matches who actually spoke, with GPU disabled (CDP `SystemInfo.getInfo` shows no hardware acceleration). Test with ≥ 10 participants so the participants panel is long enough to scroll (virtualized list). Record: URL forms that work (`*.webex.com/meet/<name>`, `*.webex.com/<site>/j.php?MTID=…`, `*.webex.com/wbxmjs/joinservice/…`), all selectors used, whether Edge policy `RemoteDebuggingAllowed` blocks us, whether headless is detected (if so, headed with `--window-position=-32000,-32000`).
- [ ] **S2 Gateway protocol. BLOCKED(Q2).** Send one Converse request with a tool definition from Go. **Done when:** `gateway.kind` + `auth` known, forced tool call returns valid Romanian JSON; max input tokens recorded.
- [ ] **S3 Transcribe Streaming, Romanian. BLOCKED(Q3b) for credentials.** Stream S1's WAV (and a 10-min RO/EN sample) to `StartStreamTranscription` in `eu-central-1` with `ro-RO`, then with `IdentifyMultipleLanguages` + `ro-RO,en-US`. **Done when:** we know which mode reads better, word timestamps are present, latency is < 5 s, and the max stream duration / reconnect behaviour is confirmed (needed for D8).
- [ ] **S4 Template fidelity.** Fill `templates/project_workbook_template.xlsx` with excelize: 60 action items, 50 participants, 15 rows in each other table. **Done when:** the file opens in Excel with no repair prompt, tables have grown with banding/styles intact, comments on Change Log headers are still there, formulas `B6`/`A2` still compute. Otherwise switch to D7 fallback.

Gates: if S1 fails on both Edge and Chrome for Testing → stop, ask the user about 9.1 (Web SDK) or D2(c). If S1 cannot read the active speaker reliably → stop, discuss speaker attribution options before M3.

### M1: Skeleton

- [x] **T1.1** *(built and cross-compiled; on-Windows render check pending, done together with S1)* `git init`, branch `feat/skeleton`, `go mod init deo-minutes`, Wails React-TS template, copy the user's master workbook to `templates/project_workbook_template.xlsx`, `.gitignore` (`build/`, `*.wav`, `.env`, `frontend/node_modules`, `/260407_*.xlsx` user originals), `CLAUDE.md` with: `gofmt`, `go vet ./...`, `golangci-lint run`, `go test ./...`, `wails build -platform windows/amd64`. Apply C7 in `main.go`. **Done when:** an empty window titled "DEO Minute" opens on Windows and renders with GPU disabled.
- [x] **T1.2** `config/`: load/save JSON, defaults from 5.1, Documents via KnownFolder. Unit tests. **Done when:** missing file → defaults written; tests pass.
- [x] **T1.3** `secrets/`: `Set/Get/Delete` over go-keyring. Frontend only ever sees "configurat / neconfigurat".
- [x] **T1.4** `i18n/`: `T(key, args...)`, `ro.json`; frontend uses the same JSON. Test: every key used in Go and TSX exists.
- [x] **T1.5** `logx/`: `log/slog` JSON to `%LOCALAPPDATA%\DEOMinutes\logs\`, rotating; `redact.go` strips keys, `Authorization`, AWS secrets, Webex link query params. Test redaction. Never log transcript text at info level.

### M2: Bot (`internal/bot`)

- [ ] **T2.1** `browser.go`: find browser (config override → Edge → Chrome for Testing in `%LOCALAPPDATA%`), launch with S1 flags, fresh profile per session, wiped after.
- [ ] **T2.2** `webex_flow.go` + `selectors.go`: validate link (https, host ends in `.webex.com`, else Romanian error), navigate, join flow with all selectors in `selectors.go`. States: prejoin, lobby, in-meeting, ended, removed. Lobby timeout → `Eroare`.
- [ ] **T2.3** `capture.js`: injected with `page.AddScriptToEvaluateOnNewDocument` before navigation. Hook `RTCPeerConnection` `track` events, mix remote audio tracks into one `MediaStreamDestination`, AudioWorklet → 16 kHz Int16, 1 s chunks (base64 + page timestamp) → binding `deoAudio`. Go side caps payload size.
- [ ] **T2.4** `roster.js`: keep the participants panel open; every 500 ms emit `{ts, participants[], activeSpeaker}` → binding `deoSpeaker`. Handle the virtualized list (scroll to collect all 50 names once, then track joins/leaves). Active speaker from the stage label / speaking indicator found in S1.
- [ ] **T2.5** End detection: "meeting ended" DOM, removed by host, page closed, or no audio energy for `silence_end_min` (default 10, long breaks happen in 4 h meetings). **Oprește** leaves cleanly, pipeline continues.
- [ ] **T2.6** Optional chat notice (`post_chat_notice`, default off since the user informs participants). **Done when (M2):** on Windows, the app joins a test meeting, shows live state, logs roster + speaker timeline, records audio, and leaves.

### M3: Transcription + speakers

- [ ] **T3.1** `stt/streamer.go`: PCM channel → `StartStreamTranscription` (16 kHz, PCM, `ro-RO` or multi-language per S3), partial results ignored, final results kept with word-level timestamps. **BLOCKED(Q3b)** for live credentials; unit-test against a fake stream.
- [ ] **T3.2** `stt/rotate.go`: open a new stream every `rotate_min` (and on error), overlap 2 s, dedupe words by timestamp. Network drop: buffer PCM on disk, reconnect with backoff, replay. Test: simulated disconnect mid-sentence loses no words.
- [ ] **T3.3** `speakers/align.go`: assign each word to the active speaker at its timestamp (majority over the word's span; overlapping speech → speaker with most time); merge consecutive words into lines; unknown → "Participant necunoscut". Golden test with `testdata/words.json` + `speaker_events.json`.
- [ ] **T3.4** Write `_transcriere.txt` incrementally (crash-safe); delete raw audio after success unless `keep_audio`.

### M4: LLM extraction

- [ ] **T4.1** `llm/client.go`: `Extract(ctx, system, user string, tool ToolSpec) (json.RawMessage, error)`. `bedrock.go` via `bedrockruntime.Converse`, `BaseEndpoint = gateway.base_url`, forced `ToolChoice`, temperature 0, retries on 429/5xx, 120 s timeout. `fake.go` for tests. **BLOCKED(Q2)** for the real endpoint only.
- [ ] **T4.2** `prompts/ro_extract.md` (Romanian system prompt): role, meeting date, roster, the six record types with field definitions from 5.2/5.3, enum lists, evidence rule, "extract only what was said", examples of each record type (short).
- [ ] **T4.3** `chunk.go`: ~30-minute windows on line boundaries, 2 min overlap. `ro_merge.md`: merge duplicates across windows, keep earliest evidence plus later ones, link interdependencies to action/risk keys.
- [ ] **T4.4** `validate.go` per 5.3 rules. Tests for each rule, including a hallucinated quote that must be dropped.
- [ ] **T4.5** Extractor test: fake client + `testdata/transcript_ro_sample.txt` → `expected_records.json`.

### M5: Workbook export

- [ ] **T5.1** `export/workbook.go`: open embedded template, write `Meetings` cells and participants (`Local/Call` = `Call` for everyone in the Webex roster), write each register, IDs `AI-1…`, `ID-1…`, `R-1…`, `D-1…`, `CH-1…`, resolve cross-refs, evidence into Notes.
- [ ] **T5.2** `export/tables.go`: grow tables per 5.2 "Capacity"; clear unused prefilled IDs beyond our rows (leave the rows, blank the `#` cell) so the sheet does not show empty `AI-17` rows.
- [ ] **T5.3** `sanitize.go`: formula-injection guard, strip control chars, cap cell length at 32,767.
- [ ] **T5.4** Golden test: fixed records → read back with excelize and compare cell values, table refs, and sheet list. **Done when (M5):** `go test ./...` green and a real file opens in Excel (RO locale) without repair, diacritics intact. **Q13 may adjust this milestone.**

### M6: UI (Romanian)

- [ ] **T6.1** Main page: link, Proiect (remembered), Nr. întâlnire (auto-suggest last + 1 per project), Întocmit de (remembered), **Pornește** / **Oprește**, state badge, elapsed timer, live transcript tail with speaker names, participant count; on finish **Deschide Excel** and **Deschide folderul**.
- [ ] **T6.2** Setări: gateway fields + key (write-only, shows "Salvat"), AWS credentials for Transcribe (write-only), region, **Testează conexiunea** (gateway + Transcribe 2 s silence), output folder, retention.
- [ ] **T6.3** Istoric: past meetings (date, project, no., item counts), open/delete; retention cleanup on startup.
- [ ] **T6.4** Romanian error catalogue in `ro.json` for every failure path: link invalid, browser not found, policy blocked, lobby timeout, removed by host, no audio, Transcribe auth/region/network, gateway auth/timeout/limit, Excel write failed / file open in Excel. No stack traces in UI.

### M7: Packaging

- [ ] **T7.1** `scripts/build-windows.sh`: `wails build -platform windows/amd64 -trimpath -ldflags "-s -w -X main.version=$VER"` → `DEOMinute.exe` + zip. No installer.
- [ ] **T7.2** `.github/workflows/build.yml` on `windows-latest`: vet, lint, test, build, upload artifact.
- [ ] **T7.3** ~~Code signing~~ not needed (Q5: unsigned exe runs). Only document the SmartScreen "Run anyway" click in the README if it appears.
- [ ] **T7.4** `README.md` in Romanian (load `unslop` first, per global CLAUDE.md).

### M8: Hardening

- [ ] **T8.1** Security review (`/cso` or `security-review`): link validation, secrets never logged or in `meta.json`, formula injection, binding payload limits, browser profile wiped, TLS verification on everywhere.
- [ ] **T8.2** Crash recovery: on restart, offer "Reia procesarea" for sessions with a transcript but no Excel.
- [ ] **T8.3** Load test: replay a 4 h, 50-speaker synthetic session (recorded audio looped + generated speaker events) end to end; memory stays < 500 MB, Excel ready < 5 min after end.
- [ ] **T8.4** Manual acceptance (section 8).

### M9: Later (not v1)

- [ ] **T9.2** `Team` column (Q14): person → team mapping editable in Setări, filled from `Responsible`.
- [ ] **T9.1** Shared room devices (Q15): enable Transcribe speaker partitioning, split one Webex participant into `<participant> (vorbitor N)` sub-speakers.

## 7. Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Webex web UI changes break selectors | High over time | All selectors in `selectors.go`; `DEOMinute.exe --selftest <link>`; fallback 9.1. |
| Active speaker not readable from the DOM, or wrong with overlapping speech | Medium | S1 gate; majority-vote alignment; worst case label lines by Transcribe speaker labels and let the LLM keep names only where the roster confirms them. |
| Several people share one room device / laptop | Possible (Q15) | Accepted for v1: lines attributed to the room. T9.1 later. |
| Webex detects headless and blocks guests | Medium | Headed off-screen window; fallback 9.1. |
| IT blocks Edge remote debugging | Medium | Chrome for Testing in `%LOCALAPPDATA%`; checked in S1. (Unsigned exe confirmed OK, Q5.) |
| Host does not admit the bot | Low (own meetings) | Lobby timeout + clear message. |
| Automating the Webex web client conflicts with Webex terms | Unknown | Accepted by user (Q6 = easiest); 9.1 is the sanctioned route if challenged. |
| Network drop during a 4 h meeting | Medium | Disk-buffered PCM + replay on reconnect (T3.2). |
| excelize damages template tables | Low-Medium | S4; XML-patch fallback (D7). |
| LLM invents items | Medium | Evidence validation, temperature 0, evidence visible in Notes. |
| Gateway not ready (Q2) and no AWS account (Q3b) | Certain for now | Build against `fake.go` and a fake Transcribe stream; S1 and S4 do not need AWS. End-to-end testing waits for 9.2. |

## 8. Acceptance checklist (v1)

- [ ] Fresh non-admin Windows 11 user: unzip, run `DEOMinute.exe`, no UAC prompt at any point.
- [ ] Paste link → bot visible as "Asistent Minute (înregistrează)" → host admits → recording state shown.
- [ ] 4-hour, 50-person RO/EN meeting (or T8.3 replay) → Excel in `Documents\DEO Minute` within 5 min of end, opens without repair, all six sheets in template layout.
- [ ] All 50 participants listed on `Meetings`; project, meeting no., date, prepared-by filled.
- [ ] Transcript lines carry real names; spot check 20 lines, ≥ 90% correct speaker.
- [ ] Every register row has evidence in Notes that is findable in the transcript.
- [ ] No GPU use (C7) in app window or bot browser.
- [ ] No secret in logs, `meta.json`, `config.json`.
- [ ] Every UI string and error is Romanian.

## 9. Open questions for the user

Status key: **OPEN** / **ANSWERED** / **N/A**. When answered, update the row, affected
tasks, and the Decision log.

| ID | Status | Question / answer | Blocks | Default until answered |
|----|--------|-------------------|--------|------------------------|
| Q1 | ANSWERED | Template = `260407_Project workbook_MASTER.xlsx` (6 sheets, mapped in 5.2). | — | — |
| Q2 | OPEN | Bedrock gateway is **not set up yet**. When ready we need: base URL; protocol (native Bedrock `Converse` vs OpenAI-compatible vs custom); auth (API key + header name, or AWS keys/SigV4, or SSO); allowed model IDs; region / EU data residency; context and rate limits; tool-use / JSON-schema support; proxy or VPN needs; per-user or shared credentials. Also: until it exists, may we test against Bedrock directly in an AWS account? | S2, T4.1 real calls | Fake client |
| Q3 | ANSWERED | Use Amazon Transcribe. | — | — |
| Q3b | OPEN | **User has no AWS set up yet (2026-10-26).** Needed before any live transcription: see 9.2 for the exact AWS setup checklist to hand to whoever owns AWS. | S3, T3.1 real calls, all end-to-end tests | Build against fake stream; plug in when 9.2 is done |
| Q4 | ANSWERED | Up to 4 h, 50 participants, Romanian + English, speaker names required. | — | — |
| Q5 | PARTIAL | **Answered:** laptops can run the unsigned `.exe` (no signing needed). **Still unknown, verify in S1 instead of asking:** Edge `RemoteDebuggingAllowed` policy, HTTPS proxy with TLS inspection. | S1 | No TLS inspection |
| Q6 | ANSWERED | Easiest / fewest permissions → **option A** (web client automation). | — | — |
| Q7 | ANSWERED | Own company's meetings, guests allowed, lobby on, no password, no webinars. | — | — |
| Q8 | N/A | Only for option B. | — | — |
| Q9 | ANSWERED | User informs participants. Chat notice off by default. | — | — |
| Q10 | ANSWERED | One meeting per laptop; app runs on the laptop; Excel goes to Documents. | — | — |
| Q11 | ANSWERED | **New workbook per meeting.** No append to MASTER. | — | — |
| Q12 | ANSWERED | **English** for template headers, sheet names and enum values (`Low/Medium/High`, `New/Approved/…`, `Active`, `Call`, `Open`). Free-text content (descriptions, rationale, notes) stays Romanian (C3). | — | — |
| Q13 | OPEN | Allowed values for: Action Item `Status`; Risk `Severity` and `Probability` (Low/Medium/High or 1-5?); Interdependency `Status`; Change impact columns; `Local/Call` (is it `Local` / `Call` / `No` = absent?). | 5.2, T4.4 | See 5.2 defaults |
| Q14 | ANSWERED | **Leave `Team` (Action Item Register, col F) empty in v1.** Do not ask the LLM for it; do not fill it even if a team is spoken. Possible later: person → team mapping in Setări (T9.2). | — | — |
| Q15 | ANSWERED (deferred) | Shared room devices are possible, but **out of scope for v1**. v1 labels lines with the Webex participant name (e.g. the room's name). Later: Transcribe speaker labels inside that participant ("Sala X (vorbitor 2)"), task T9.1. | — | — |
| Q16 | ANSWERED | **No narrative summary.** Only the template sheets. | — | — |

### 9.2 AWS setup checklist (blocks live Transcribe; hand this to whoever owns AWS)

1. An AWS account for the company (or an existing one), region **`eu-central-1` (Frankfurt)** so data stays in the EU. `ro-RO` streaming is available there.
2. Amazon Transcribe enabled (no extra setup; it is on by default in the account).
3. An IAM policy allowing only streaming transcription:
   ```json
   { "Version": "2012-10-17",
     "Statement": [{ "Effect": "Allow",
       "Action": ["transcribe:StartStreamTranscription", "transcribe:StartStreamTranscriptionWebSocket"],
       "Resource": "*" }] }
   ```
4. Credentials for the laptops, preferred first: (a) IAM Identity Center (SSO) users with that policy, short-lived credentials; (b) one IAM user per person with access keys (rotated); (c) one shared IAM user (simplest, weakest; treat keys as compromised if a laptop is lost). The app stores whichever it gets in Windows Credential Manager.
5. Billing alert / budget on the account (Transcribe Streaming is billed per second of audio; a 4 h meeting is 14,400 s).
6. If the Bedrock gateway (Q2) will live in the same account, the same person can set both up; Bedrock needs `bedrock:InvokeModel` / `bedrock:Converse` on the chosen model and model access enabled in the Bedrock console.
7. DPO / data-protection sign-off for sending meeting audio and transcripts to AWS.

### 9.1 Option B reference: Webex Web SDK (only if option A fails)

- **Auth:** a Webex **Service App** (Guest Issuer apps are deprecated/removed) with scopes `guest-issuer:write` + `guest-issuer:read`, authorized by a Control Hub admin. Only paid Webex orgs can create guests. The app calls `POST /v1/guests/token` (subject + display name) to get a guest access token.
- **Secret handling:** the Service App secret should not sit on every laptop. Either store it in Credential Manager per user (small trusted group) or run a small internal token service handing out short-lived guest tokens.
- **Join:** a local page bundled in the exe loads the `webex` JS SDK (pinned, bundled), `webex.meetings.create(link)` → `meeting.join()`; remote audio arrives as a `MediaStream` on `media:ready` with `type === 'remoteAudio'`. No UI scraping for audio.
- **Unchanged:** lobby, Transcribe, LLM, Excel, UI. Speaker attribution still needs verification with the SDK's member/active-speaker events.
- **Cost:** Webex admin involvement + about 3-5 extra dev days.

## 10. Decision log

| Date | ID | Decision / finding |
|------|----|--------------------|
| 2026-10-26 | D1-D7 | Initial plan written. |
| 2026-10-26 | C7 | User: laptops do not permit GPU acceleration. Disable GPU in WebView2 (Wails) and bot browser. |
| 2026-10-26 | Q1 | Template provided: `260407_Project workbook_MASTER.xlsx`. Output switched from CSV to filling this XLSX (C8, D7, 5.2). |
| 2026-10-26 | Q3 | Amazon Transcribe Streaming (`ro-RO`, supported for streaming per AWS docs) replaces local whisper as the main STT (D4). Removes laptop CPU load. |
| 2026-10-26 | Q4 | 4 h / 50 people / RO+EN / speaker names → speaker attribution from Webex active speaker (D5), stream rotation + map/reduce (D8), table growth (5.2). |
| 2026-10-26 | Q6 | Option A (Webex web client automation) chosen: easiest, fewest permissions. |
| 2026-10-26 | Q5, Q11, Q16 | Unsigned exe runs on laptops (no signing). New workbook per meeting. No narrative summary. |
| 2026-10-26 | Q12, Q15, Q3b | English headers/enums, Romanian content. Shared room devices deferred to T9.1. No AWS yet: AWS checklist added (9.2); live STT/LLM work runs against fakes. |
| 2026-10-26 | Q13, Q14 | Q13 defaults accepted for now (5.2). `Team` column left empty in v1 (T9.2 later). |
| 2026-10-26 | Q7, Q9, Q10 | Own-company meetings, guests allowed, lobby on, no password; user informs participants; output to Documents. |
