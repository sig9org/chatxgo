# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

chatxgo is a Go CLI/library that sends a single Markdown-formatted message (subject, body, mentions, attachments) to whichever of Cisco Webex, Microsoft Teams, and Slack are configured. It fans a single `notify.Message` out to every enabled `notify.Sender` and reports one `notify.Result` per tool — a failure on one tool does not stop delivery to the others.

## Commands

This project uses [Task](https://taskfile.dev) (`Taskfile.yml`), not raw `go` commands, for the common workflows:

```
task build          # go build for the current platform -> dist/chatxgo(.exe)
task build-all       # cross-compile release binaries for all platforms into dist/
task test            # go test ./...
task run -- <args>   # go run . <args>, e.g. task run -- -subject "hi" -body "hi"
task debug -- <args> # same as run, with -debug forced on
task version         # go run . -v
task cleanup         # remove OS/editor cruft (.DS_Store, .idea, etc.)
```

Run a single test with plain `go test`, e.g.:

```
go test ./notify/ -run TestTeamsCard -v
```

`task build`/`build-all`/`run`/`debug`/`version` inject the version string via `-ldflags -X .../internal/version.Version=<git tag>` (falls back to `dev`); prefer the Task targets over bare `go build`/`go run` if version output matters.

## Architecture

**Two layers, deliberately separated:** `main.go` is a thin CLI (flag parsing, config file resolution, exit codes) over the `notify` package, which is a standalone library (`github.com/sig9org/chatxgo/notify`) importable by other Go programs independent of the CLI.

**Core flow** (`notify/notify.go`):
- `Config` aggregates per-tool settings (`WebexConfig`, `TeamsConfig`, `SlackConfig`). A tool is enabled purely by having a non-empty `Dest` — there is no separate on/off flag.
- `Senders(cfg)` builds the enabled `Sender`s in a fixed order (Webex, Teams, Slack); `NewDispatcher(cfg)` wraps that list.
- `Dispatcher.Send` validates the `Message` once, then calls each `Sender.Send` in turn, collecting a `Result{Tool, Err}` per sender — no partial-message semantics, no early abort on error.
- Every chat tool implements the same `Sender` interface (`Name() string`, `Send(ctx, Message) error`); adding a new destination means adding a new file in `notify/` plus one line in `Senders()` and a matching `*Config` field on `Config`.

**Per-tool payload construction is where the tools diverge** — each has its own mention syntax and Markdown dialect, all built from the same `notify.Message`:
- `notify/webex.go`: posts Markdown to the Webex Messages API. Mentions become `<@personId:ID|label>` or `<@personEmail:ID|label>` (auto-detected by whether `Mention.ID` contains `@`). Local file attachments are uploaded via multipart POST; URL attachments are passed as `files` in the JSON body.
- `notify/teams.go`: posts to a Teams incoming webhook as an **Adaptive Card** (`type: "message"`, `attachments[].content` = `AdaptiveCard`), not the legacy MessageCard format — MessageCard mentions don't reliably resolve. Mentions use the `msteams.entities` array on the card content, with `mentioned.id` set to `Mention.ID`; Teams Incoming Webhooks accept either a Microsoft Entra object ID (GUID) or a user principal name/email there. Adaptive Card `TextBlock.text` only supports a CommonMark subset (bold, italic, lists, links — no headings/tables/images/code blocks), so don't rely on other Markdown in Teams bodies. There is no file upload for Teams; attachments are rendered as Markdown links in the body text.
- `notify/slack.go`: posts `{"text": ...}` to a Slack incoming webhook. Mentions become `<@ID>` (Slack resolves the ID at render time — no email fallback). If `SlackConfig.Token` and `Channel` are both set, local file attachments are uploaded via Slack's 3-step `files.getUploadURLExternal` → external PUT → `files.completeUploadExternal` flow instead of being turned into links; URL attachments are always rendered as links regardless of token/channel.
- `notify/attachment.go` holds the shared helpers used by all three senders: `isURL`, `formatAttachmentLine` (Markdown bullet fallback), `readLocalAttachment`.

**Config resolution** (`notify/configfile.go`): `config.ini` is a standard INI file where each `[section]` is a named profile; keys before any section header live in the implicit `ini.DefaultSection` and serve as the fallback for the `"default"` profile only (requesting any other missing profile is an error — see `configSection`). `DefaultConfigPath` checks `./config.ini` first, then a per-OS user config dir. Library callers only get this file-based config if they explicitly call `LoadConfigFile`; constructing `Config` directly always takes precedence.

**Mention parsing** (`notify.ParseMention`, used by the CLI) accepts `"id"` or `"id:label"` — the label is optional and, if empty, `Mention.label()` falls back to displaying the ID.

**`internal/` packages** are CLI-only support code, not part of the public `notify` API surface: `internal/debugx` (a process-wide atomic-bool toggle plus `Printf`, gated by `-debug`), `internal/version` (name/version string, `Version` overwritten at build time), `internal/selfupdate` (wraps `creativeprojects/go-selfupdate` against the `sig9org/chatxgo` GitHub repo).

**CLI flag conventions** (`main.go`): every flag has a long and short form registered as separate `flag.FlagSet` vars pointing at the same struct field; `flagHelps` exists solely so `printUsage` can print short/long pairs on one line instead of the flag package's default one-line-per-name. `stringList` is a custom `flag.Value` so `-mention`/`-attach` can be repeated (`-m a -m b`) and/or comma-separated (`-m a,b`) interchangeably.

## Testing conventions

Each `notify/*.go` sender file has a matching `_test.go` using `httptest.NewServer` to assert on the actual JSON/form payload sent over the wire (not just that `Send` returns nil) — follow this pattern (decode the request body into the same struct the sender builds, then assert fields) when changing a sender's payload shape.
