// Command chatxgo sends Markdown-formatted notifications to Cisco Webex,
// Microsoft Teams, and Slack.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sig9org/chatxgo/internal/colorx"
	"github.com/sig9org/chatxgo/internal/debugx"
	"github.com/sig9org/chatxgo/internal/selfupdate"
	"github.com/sig9org/chatxgo/internal/version"
	"github.com/sig9org/chatxgo/notify"
)

// stringList is a flag.Value that collects repeated "-flag a -flag b" uses
// as well as comma-separated "-flag a,b" values into a single slice.
type stringList struct {
	values []string
}

func (l *stringList) String() string { return strings.Join(l.values, ",") }

func (l *stringList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			l.values = append(l.values, part)
		}
	}
	return nil
}

type cli struct {
	update      bool
	showVersion bool
	showHelp    bool
	dryrun      bool
	debug       bool
	silent      bool
	configFile  string
	profile     string
	proxy       string
	subject     string
	body        string
	mentions    stringList
	attachments stringList
}

// flagHelp documents one logical option (its short and/or long flag names
// together) for printUsage. The flag package prints each registered name as
// its own line, which would show short/long aliases as separate entries;
// flagHelps lets printUsage render them as a single grouped line instead.
type flagHelp struct {
	names       []string
	placeholder string // "" for bool flags
	usage       string
}

var flagHelps = []flagHelp{
	{[]string{"update"}, "", "Update chatxgo to the latest release"},
	{[]string{"v", "version"}, "", "Show version information"},
	{[]string{"h", "help"}, "", "Show usage information"},
	{[]string{"dryrun"}, "", "Validate and report what would be sent, without actually sending"},
	{[]string{"debug"}, "", "Print verbose debug output"},
	{[]string{"silent"}, "", "Suppress normal stdout messages (overridden by -debug)"},
	{[]string{"config"}, "string", "Path to the config.ini file with chat tool credentials (default: config.ini in the current directory, falling back to the per-user config directory)"},
	{[]string{"p", "profile"}, "string", "Profile (config.ini section) to use (default: \"default\")"},
	{[]string{"proxy"}, "string", "HTTP(S) proxy URL to route chat tool requests through (overrides PROXY in config.ini)"},
	{[]string{"s", "subject"}, "string", "Message subject/title"},
	{[]string{"b", "body"}, "string", "Message body, formatted as Markdown"},
	{[]string{"m", "mention"}, "value", "User to mention, as \"id\" or \"id:label\" (repeatable, or comma-separated)"},
	{[]string{"a", "attach"}, "value", "File path or URL to attach (repeatable, or comma-separated)"},
}

func parseFlags(args []string, errOutput io.Writer, out *cli) (*flag.FlagSet, error) {
	fs := flag.NewFlagSet(version.Name, flag.ContinueOnError)
	fs.SetOutput(errOutput)

	fs.BoolVar(&out.update, "update", false, "")
	fs.BoolVar(&out.showVersion, "v", false, "")
	fs.BoolVar(&out.showVersion, "version", false, "")
	fs.BoolVar(&out.showHelp, "h", false, "")
	fs.BoolVar(&out.showHelp, "help", false, "")
	fs.BoolVar(&out.dryrun, "dryrun", false, "")
	fs.BoolVar(&out.debug, "debug", false, "")
	fs.BoolVar(&out.silent, "silent", false, "")
	fs.StringVar(&out.configFile, "config", "", "")
	fs.StringVar(&out.profile, "profile", "", "")
	fs.StringVar(&out.profile, "p", "", "")
	fs.StringVar(&out.proxy, "proxy", "", "")
	fs.StringVar(&out.subject, "subject", "", "")
	fs.StringVar(&out.subject, "s", "", "")
	fs.StringVar(&out.body, "body", "", "")
	fs.StringVar(&out.body, "b", "", "")
	fs.Var(&out.mentions, "mention", "")
	fs.Var(&out.mentions, "m", "")
	fs.Var(&out.attachments, "attach", "")
	fs.Var(&out.attachments, "a", "")

	fs.Usage = func() { printUsage(fs) }

	err := fs.Parse(args)
	return fs, err
}

func printUsage(fs *flag.FlagSet) {
	fmt.Fprintln(fs.Output(), version.String())
	fmt.Fprintln(fs.Output())
	fmt.Fprintf(fs.Output(), "Usage: %s [options]\n\n", version.Name)
	fmt.Fprintln(fs.Output(), "Sends a Markdown-formatted message to every chat tool enabled in")
	fmt.Fprintln(fs.Output(), "config.ini (Cisco Webex, Microsoft Teams, Slack).")
	fmt.Fprintln(fs.Output())
	fmt.Fprintln(fs.Output(), "Options:")
	for _, h := range flagHelps {
		head := "  -" + strings.Join(h.names, ", -")
		if h.placeholder != "" {
			head += " " + h.placeholder
		}
		fmt.Fprintln(fs.Output(), head)
		fmt.Fprintf(fs.Output(), "    \t%s\n", h.usage)
	}
	fmt.Fprintln(fs.Output())
	fmt.Fprintf(fs.Output(), "Example:\n  %s -subject \"Deploy done\" -body \"**v1.2.3** shipped\" -mention U0123456\n", version.Name)
}

// printError writes args to w as a single line, colored red, matching the
// spacing fmt.Fprintln would have produced uncolored.
func printError(w io.Writer, args ...any) {
	fmt.Fprintln(w, colorx.Red(strings.TrimSuffix(fmt.Sprintln(args...), "\n")))
}

// printErrorf writes a red-colored formatted error line to w.
func printErrorf(w io.Writer, format string, args ...any) {
	fmt.Fprintln(w, colorx.Red(fmt.Sprintf(format, args...)))
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	var c cli
	fs, err := parseFlags(args, stderr, &c)
	if err != nil {
		return 2
	}

	debugx.Enable(c.debug)
	debugx.Writer = stdout

	if c.showHelp {
		fs.SetOutput(stdout)
		printUsage(fs)
		return 0
	}
	if c.showVersion {
		fmt.Fprintln(stdout, version.String())
		return 0
	}

	// -debug takes priority over -silent: verbose output always wins.
	silent := c.silent && !c.debug

	if c.update {
		return doUpdate(stdout, stderr, silent)
	}

	return doSend(&c, fs, stdout, stderr, silent)
}

func doUpdate(stdout, stderr io.Writer, silent bool) int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	msg, err := selfupdate.Update(ctx, version.Version)
	if err != nil {
		printError(stderr, "error:", err)
		return 1
	}
	if !silent {
		fmt.Fprintln(stdout, msg)
	}
	return 0
}

// loadConfig resolves and loads the config.ini file to use. If path is
// empty, it is resolved via notify.DefaultConfigPath (config.ini in the
// current directory, falling back to the per-user config directory).
// profile selects which section of the file to read; an empty profile
// means notify.DefaultProfile. A missing config file is not an error: it
// simply leaves every chat tool disabled.
func loadConfig(path, profile string, stderr io.Writer) (notify.Config, error) {
	if path == "" {
		resolved, err := notify.DefaultConfigPath()
		if err != nil {
			printError(stderr, "error: resolve config path:", err)
			return notify.Config{}, err
		}
		path = resolved
	}

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			debugx.Printf("config file %s not found; no chat tool will be enabled", path)
			return notify.Config{}, nil
		}
		printError(stderr, "error: read config file:", err)
		return notify.Config{}, err
	}

	cfg, err := notify.LoadConfigFile(path, profile)
	if err != nil {
		printError(stderr, "error:", err)
		return notify.Config{}, err
	}
	debugx.Printf("loaded config file %s (profile %q)", path, profile)
	return cfg, nil
}

// redactProxy returns proxy with any embedded credentials masked, safe to
// write to debug output. It returns proxy unchanged if it doesn't parse as
// a URL (the invalid case is reported separately when the proxy is used).
func redactProxy(proxy string) string {
	if proxy == "" {
		return ""
	}
	if u, err := url.Parse(proxy); err == nil {
		return u.Redacted()
	}
	return proxy
}

// toolDisplayName maps a notify.Sender's Name() to the human-readable
// product name shown in CLI output.
func toolDisplayName(tool string) string {
	switch tool {
	case "webex":
		return "Cisco Webex"
	case "teams":
		return "Microsoft Teams"
	case "slack":
		return "Slack"
	default:
		return tool
	}
}

func doSend(c *cli, fs *flag.FlagSet, stdout, stderr io.Writer, silent bool) int {
	cfg, err := loadConfig(c.configFile, c.profile, stderr)
	if err != nil {
		return 1
	}
	if c.proxy != "" {
		cfg.Proxy = c.proxy
	}
	debugx.Printf("webex enabled=%v teams enabled=%v slack enabled=%v proxy=%q",
		cfg.Webex.Dest != "", cfg.Teams.Dest != "", cfg.Slack.Dest != "", redactProxy(cfg.Proxy))

	msg := notify.Message{
		Subject:     c.subject,
		Body:        c.body,
		Attachments: c.attachments.values,
	}
	for _, raw := range c.mentions.values {
		m, err := notify.ParseMention(raw)
		if err != nil {
			printError(stderr, "error:", err)
			return 2
		}
		msg.Mentions = append(msg.Mentions, m)
	}
	if err := msg.Validate(); err != nil {
		printError(stderr, "error:", err)
		fmt.Fprintln(stderr)
		fs.SetOutput(stderr)
		printUsage(fs)
		return 2
	}

	if c.dryrun {
		senders, err := notify.Senders(cfg)
		if err != nil {
			printError(stderr, "error:", err)
			return 1
		}
		if len(senders) == 0 {
			printError(stderr, "error:", notify.ErrNoRecipients)
			return 1
		}
		for _, s := range senders {
			debugx.Printf("dry run: would send to %s", s.Name())
			if !silent {
				fmt.Fprintf(stdout, "Would send to %s (dry run)\n", toolDisplayName(s.Name()))
			}
		}
		return 0
	}

	dispatcher, err := notify.NewDispatcher(cfg)
	if err != nil {
		printError(stderr, "error:", err)
		return 1
	}
	results, err := dispatcher.Send(context.Background(), msg)
	if err != nil {
		printError(stderr, "error:", err)
		return 1
	}

	exit := 0
	for _, r := range results {
		if r.Err != nil {
			printErrorf(stderr, "%s: %v", r.Tool, r.Err)
			exit = 1
			continue
		}
		if !silent {
			fmt.Fprintf(stdout, "Sent to %s\n", toolDisplayName(r.Tool))
		}
	}
	return exit
}
