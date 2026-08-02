<p align="center">
  <img src="https://raw.githubusercontent.com/sig9org/chatxgo/main/assets/logo.webp" alt="ChatXGo">
</p>

# ChatXGo

A lightweight Go tool for sending a single Markdown-formatted notification (subject, body, mentions, attachments) to Cisco Webex, Microsoft Teams, and Slack at once.

chatxgo can be used in two ways:

- **As a standalone CLI tool** — run the `chatxgo` binary directly from a shell script, CI job, cron task, etc.
- **As a Go library** (`github.com/sig9org/chatxgo/notify`) — import the `notify` package into your own Go program and call it directly, without going through the CLI or a config file at all.

Every chat tool is optional and independent: giving a tool's destination (`*_DST`) enables it, leaving it empty disables it, and a single `chatxgo` invocation delivers the same message to every tool that is enabled at once. A delivery failure on one tool does not stop delivery to the others.

## Setup

Copy `config.ini.example` to `config.ini` and fill in the destinations/credentials for the chat tools you want to use. A tool is enabled simply by giving it a `*_DST` value; leave it empty to disable that tool.

```
WEBEX_TOKEN=...      # Webex bot/personal access token
WEBEX_DST=...        # destination roomId

MSTEAMS_DST=...      # Teams incoming webhook URL

SLACK_DST=...        # Slack incoming webhook URL
SLACK_TOKEN=...      # optional, needed only to upload local file attachments
SLACK_CHANNEL=...    # optional, channel for uploaded attachments

PROXY=...            # optional, HTTP(S) proxy URL every enabled tool's requests are routed through
```

### Config file location and priority

When run as the CLI, chatxgo resolves `config.ini` in this order:

1. A `config.ini` in the current directory takes priority.
2. Otherwise, a per-user location is used:
   - Linux/macOS: `~/.config/chatxgo/config.ini`
   - Windows: `C:\Users\<user>\AppData\Roaming\chatxgo\config.ini`

Pass `-config /path/to/config.ini` to use a specific file instead of the resolved default. A missing config file is not an error — it simply leaves every chat tool disabled.

### Profiles

`config.ini` can hold several named profiles as INI sections, so you can keep, for example, a personal and a work destination side by side in one file:

```
[default]
SLACK_DST=https://hooks.slack.example/default

[work]
SLACK_DST=https://hooks.slack.example/work
```

Select which profile to use with `-profile`/`-p` (default: `default`). Settings written before any `[section]` header belong to the `default` profile. Requesting a profile that doesn't exist in the file is an error.

## CLI usage

```
chatxgo -subject "Deploy done" -body "**v1.2.3** shipped" -mention U0123456 -attach ./report.pdf
```

| Option | Shorthand | Description |
| --- | --- | --- |
| `-subject` | `-s` | Message subject/title |
| `-body` | `-b` | Message body, formatted as Markdown |
| `-mention` | `-m` | User to mention, as `id` or `id:label` (repeatable, or comma-separated). `id` is the native identifier for each tool: a Slack user ID, a Webex email/personId, or for Teams a Microsoft Entra object ID or user principal name/email. |
| `-attach` | `-a` | File path or URL to attach (repeatable, or comma-separated) |
| `-config` | | Path to the `config.ini` file (default: resolved as described in [Setup](#setup)) |
| `-profile` | `-p` | Profile (config.ini section) to use (default: `default`) |
| `-proxy` | | HTTP(S) proxy URL to route chat tool requests through (overrides `PROXY` in config.ini) |
| `-dryrun` | | Validate and report what would be sent, without actually sending |
| `-debug` | | Print verbose debug output (with timestamps) |
| `-silent` | | Suppress normal stdout messages; overridden by `-debug` if both are given |
| `-update` | | Self-update to the latest GitHub release |
| `-v` | `-version` | Show version information (tag plus build commit hash, e.g. `chatxgo v0.0.3 (a3a6ca6)`) |
| `-h` | `-help` | Show usage information |

A mention can be given as `id:label`, separating the identifier and the display name with a colon:

```sh
chatxgo -profile "work" -subject "Deploy done" -body "**v1.2.3** shipped" -mention "jane.doe@example.com:Jane"
```

### Dry runs

Pass `-dryrun` to validate the message and report which enabled tools it would be sent to, without actually sending it:

```sh
chatxgo -dryrun -subject "Deploy done" -body "**v1.2.3** shipped"
```

### Sending through a proxy

Every enabled tool's requests can be routed through an HTTP(S) proxy: set `PROXY` in `config.ini`, or pass `-proxy` on the command line (which takes priority over `PROXY` if both are given):

```sh
chatxgo -proxy "http://user:pass@proxy.example:8080" -subject "Deploy done" -body "**v1.2.3** shipped"
```

### Colored output

chatxgo colors terminal messages by severity: normal messages (e.g. `Sent to Slack`) are left uncolored, errors are red, and `-debug` output is gray and timestamped.

### Updating the CLI

Run `chatxgo -update` to check GitHub for a newer release of chatxgo and replace the currently running binary in place:

```sh
chatxgo -update
```

If the installed binary is already the latest version, chatxgo reports that and exits without changing anything.

### Notes on sending to Microsoft Teams

- Messages are sent to Teams as [Adaptive Cards](https://adaptivecards.microsoft.com/) rather than the legacy MessageCard format. An Adaptive Card `TextBlock` supports a subset of CommonMark Markdown (bold, italic, bullet lists, numbered lists, links); headings, tables, images, and code blocks are not supported.
- Incoming Webhooks that use Adaptive Cards officially support mentioning a user by either their Microsoft Entra object ID (a GUID) or their user principal name (UPN, typically their email address). This is why the legacy MessageCard format could not resolve email-based mentions, and why chatxgo sends Adaptive Cards instead.

## Library usage

Build a `notify.Config` directly in code; this always takes precedence, since library callers only read `config.ini` if they explicitly opt in with `notify.LoadConfigFile`. A tool is enabled by setting its `Dest`.

```go
cfg := notify.Config{
	Slack: notify.SlackConfig{Dest: "https://hooks.slack.example/..."},
	Proxy: "http://proxy.example:8080", // optional
}
// or load a specific profile (section) from a config.ini file:
// cfg, err := notify.LoadConfigFile("/path/to/config.ini", "work")
dispatcher, err := notify.NewDispatcher(cfg)
if err != nil {
	// only fails if cfg.Proxy is set but isn't a valid proxy URL
}

results, err := dispatcher.Send(ctx, notify.Message{
	Subject:     "Deploy done",
	Body:        "**v1.2.3** shipped",
	Mentions:    []notify.Mention{{ID: "U0123456"}},
	Attachments: []string{"./report.pdf"},
})
```

`Dispatcher.Send` returns one `notify.Result{Tool, Err}` per enabled tool; it returns `notify.ErrNoRecipients` if no tool is enabled at all.

## Markdown support per tool

The `-body` text is passed through to each tool largely as-is, but every chat tool renders its own dialect of Markdown, and none of them support the full CommonMark syntax. Write `-body` for the tool(s) you actually send to, and check the table below before relying on a given syntax.

| Feature | Cisco Webex | Microsoft Teams (Adaptive Cards) | Slack (mrkdwn) |
| --- | --- | --- | --- |
| Bold | `**bold**` | `**bold**` | `*bold*` — CommonMark's `**bold**` is **not** supported and shows literally |
| Italic | `_italic_` | `_italic_` | `_italic_` |
| Strikethrough | Not supported | Not supported | `~strike~` |
| Headings (`#`, `##`, ...) | `#`/`##`/`###` (h1–h3 only) | Not supported | Not supported |
| Blockquote (`>`) | Supported | Not supported | Supported |
| Unordered list | `* item` | `- item` | No native list syntax — write each line manually (e.g. `• item`) |
| Ordered list | `1. item` | `1. item` | No native list syntax — write each line manually |
| Links | `[text](url)` | `[text](url)` | **Not** `[text](url)` — must be `<url` + `\|` + `text>`, or a bare URL |
| Inline code / code block | Supported | Not supported | Supported |
| Horizontal rule (`---`) | Supported | Not supported | Not supported |
| Tables | Not supported | Not supported | Not supported |
| Images | Not supported inline via Markdown | Not supported | Not supported inline (requires Slack Block Kit, not used by chatxgo) |

Notable pitfalls:

- **Slack does not speak CommonMark.** Slack's `mrkdwn` dialect uses single asterisks for bold and `<url|text>` for links, not double asterisks or `[text](url)`. A `-body` written in standard Markdown (as you would for Webex or Teams) will show its literal `**`/`[]()` characters in Slack instead of being rendered.
- **Tables are unsupported everywhere** — Webex, Teams, and Slack all lack table rendering in the message formats chatxgo uses.
- **Teams (Adaptive Cards) has the narrowest subset**: only bold, italic, lists, and links render; headings, blockquotes, code blocks, horizontal rules, and tables are all shown as plain/literal text or dropped.

## Development

```
task go-build          # build for the current platform
task go-test           # run the test suite
go run . -debug        # run with -debug
```

## License

[MIT](./LICENSE)
