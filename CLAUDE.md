# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

chatxgo is a Go CLI/library that sends a single Markdown-formatted message (subject, body, mentions, attachments) to whichever of Cisco Webex, Microsoft Teams, Slack, and Discord are configured. It fans a single `notify.Message` out to every enabled `notify.Sender` and reports one `notify.Result` per tool — a failure on one tool does not stop delivery to the others.

## Commands

This project uses [Task](https://taskfile.dev) (`Taskfile.yml`), not raw `go` commands, for the common workflows:

```
task go-build     # go build for the current platform -> dist/chatxgo(.exe)
task go-all-build  # cross-compile release binaries for all platforms into dist/
task go-test       # go vet ./... && go test ./...
task go-version    # go run . -v
task go-register   # request pkg.go.dev to index the latest tagged version
task go-clean      # empty out dist/
task cleanup      # remove OS/editor cruft (.DS_Store, .idea, etc.)
```

Run a single test with plain `go test`, e.g.:

```
go test ./notify/ -run TestTeamsCard -v
```

The `go-*` tasks are shared definitions pulled from `sig9org/tasks` (`Taskfile.yml`'s `includes:`), driven by `GITHUB_USER`/`GITHUB_REPO`/`BINARY_NAME` in `project.ini` — including version-string injection via `-ldflags -X .../internal/version.Version=<git tag>` (falls back to `dev`) for `go-build`/`go-all-build`/`go-version`. For running the CLI directly during development, use plain `go run . <args>` (e.g. `go run . -debug -dryrun -subject "hi" -body "hi"`); `cleanup` is the only remaining project-local task.

## Architecture

**Two layers, deliberately separated:** `main.go` is a thin CLI (flag parsing, config file resolution, exit codes) over the `notify` package, which is a standalone library (`github.com/sig9org/chatxgo/notify`) importable by other Go programs independent of the CLI.

**Core flow** (`notify/notify.go`):
- `Config` aggregates per-tool settings (`WebexConfig`, `TeamsConfig`, `SlackConfig`, `DiscordConfig`) plus a top-level `Proxy` (an HTTP(S) proxy URL applied to every enabled tool's requests, not a per-tool setting). A tool is enabled purely by having a non-empty `Dest` — there is no separate on/off flag.
- `Senders(cfg) ([]Sender, error)` builds the enabled `Sender`s in a fixed order (Webex, Teams, Slack, Discord), each sharing one `*http.Client` built from `cfg.Proxy` (see `notify/httpclient.go`'s `proxyHTTPClient`); it errors if `Proxy` is set but doesn't parse as a URL. `NewDispatcher(cfg) (*Dispatcher, error)` wraps that call, so it fails the same way.
- `Dispatcher.Send` validates the `Message` once, then calls each `Sender.Send` in turn, collecting a `Result{Tool, Err}` per sender — no partial-message semantics, no early abort on error.
- Every chat tool implements the same `Sender` interface (`Name() string`, `Send(ctx, Message) error`); adding a new destination means adding a new file in `notify/` plus one line in `Senders()` and a matching `*Config` field on `Config`. Each sender's `new*Sender` constructor defaults `client` to `http.DefaultClient`; `Senders()` overwrites it with the shared proxy-aware client after construction (same package, so the unexported field is directly settable).

**Per-tool payload construction is where the tools diverge** — each has its own mention syntax and Markdown dialect, all built from the same `notify.Message`:
- `notify/webex.go`: posts Markdown to the Webex Messages API. Mentions become `<@personId:ID|label>` or `<@personEmail:ID|label>` (auto-detected by whether `Mention.ID` contains `@`). Local file attachments are uploaded via multipart POST; URL attachments are passed as `files` in the JSON body.
- `notify/teams.go`: posts to a Teams incoming webhook as an **Adaptive Card** (`type: "message"`, `attachments[].content` = `AdaptiveCard`), not the legacy MessageCard format — MessageCard mentions don't reliably resolve. Mentions use the `msteams.entities` array on the card content, with `mentioned.id` set to `Mention.ID`; Teams Incoming Webhooks accept either a Microsoft Entra object ID (GUID) or a user principal name/email there. Adaptive Card `TextBlock.text` only supports a CommonMark subset (bold, italic, lists, links — no headings/tables/images/code blocks), so don't rely on other Markdown in Teams bodies. There is no file upload for Teams; attachments are rendered as Markdown links in the body text.
- `notify/slack.go`: posts `{"text": ...}` to a Slack incoming webhook. Mentions become `<@ID>` (Slack resolves the ID at render time — no email fallback). If `SlackConfig.Token` and `Channel` are both set, local file attachments are uploaded via Slack's 3-step `files.getUploadURLExternal` → external PUT → `files.completeUploadExternal` flow instead of being turned into links; URL attachments are always rendered as links regardless of token/channel.
- `notify/discord.go`: posts to a Discord incoming webhook. Mentions become `<@ID>` and `allowed_mentions.users` restricts notifications to the explicitly supplied IDs. Local attachments are included as `files[n]` multipart fields; URL attachments are rendered as Markdown links. Discord's 2,000-character content limit is checked before sending.
- `notify/attachment.go` holds the shared helpers used by all four senders: `isURL`, `formatAttachmentLine` (Markdown bullet fallback), `readLocalAttachment`.

**Config resolution** (`notify/configfile.go`): `config.toml` is a TOML file where each table (`[default]`, `[work]`, etc.) is a named profile. All settings must be inside a profile table; an empty `-profile` selects `[default]`, and any missing requested profile is an error. Decoding uses `pelletier/go-toml/v2` with unknown fields rejected, while profile-name lookup remains case-insensitive. `DefaultConfigPath` checks `./config.toml` first, then a per-OS user config dir. Library callers only get this file-based config if they explicitly call `LoadConfigFile`; constructing `Config` directly always takes precedence. `PROXY` is read per profile like every other key.

**Mention parsing** (`notify.ParseMention`, used by the CLI) accepts `"id"` or `"id:label"` — the label is optional and, if empty, `Mention.label()` falls back to displaying the ID.

**`internal/` packages** are CLI-only support code, not part of the public `notify` API surface: `internal/debugx` (a process-wide atomic-bool toggle plus `Printf`, gated by `-debug`, gray-colored via `colorx`), `internal/colorx` (ANSI color wrappers — gray for debug, red for errors, orange for warnings; normal messages stay uncolored), `internal/version` (name/version string; `Version` is overwritten at build time via ldflags with the git tag), `internal/selfupdate` (queries the `sig9org/chatxgo` GitHub Releases API directly and uses only `creativeprojects/go-selfupdate/update` for safe binary replacement; the root package is intentionally avoided because it imports the unmaintained OpenPGP implementation).

**CLI flag conventions** (`main.go`): most flags have a long and short form registered as separate `flag.FlagSet` vars pointing at the same struct field; `flagHelps` exists solely so `printUsage` can print short/long pairs on one line instead of the flag package's default one-line-per-name. `-dryrun`, `-silent`, and `-proxy` are long-form only (no short alias — `-s` is already `-subject`'s). `stringList` is a custom `flag.Value` so `-mention`/`-attach` can be repeated (`-m a -m b`) and/or comma-separated (`-m a,b`) interchangeably. `-dryrun` validates the message and reports which enabled tools it would go to (via `notify.Senders`) without calling `Dispatcher.Send`. `-silent` suppresses normal stdout messages ("Sent to ...", the self-update result); `-debug` always overrides it (`silent := c.silent && !c.debug` in `run`), since debug output writes to the same stdout regardless of the silent flag. `-proxy` overrides `PROXY` from `config.toml` (`doSend` sets `cfg.Proxy = c.proxy` after `loadConfig` only if the flag was given); `redactProxy` masks any embedded proxy credentials before they hit `-debug` output.

## Testing conventions

Each `notify/*.go` sender file has a matching `_test.go` using `httptest.NewServer` to assert on the actual JSON/form payload sent over the wire (not just that `Send` returns nil) — follow this pattern (decode the request body into the same struct the sender builds, then assert fields) when changing a sender's payload shape.

`main_test.go` drives the CLI through `run(args, stdout, stderr io.Writer)` (never `main`/`os.Exit`), asserting on exit codes and the captured `bytes.Buffer`s. Tests that touch config-file resolution use `t.Chdir` (and `t.Setenv("HOME"/"USERPROFILE")` for the per-user config dir case) to isolate `notify.DefaultConfigPath` from the real filesystem/environment.
