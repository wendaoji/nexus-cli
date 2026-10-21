package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/wendaoji/nexus-cli/internal/config"
	"github.com/wendaoji/nexus-cli/internal/i18n"
	"github.com/wendaoji/nexus-cli/internal/nexus"
)

const loginUsageEN = `Usage: nexus-cli login --user <account> [--password <password> | --password-stdin]
                      [--url <url>] [--context <name>]

Stores a target address and credentials as a context in
$XDG_CONFIG_HOME/nexus-cli/config.json (mode 0600), after validating them.

  --user <account>       Account to store (a dedicated read-only account is best)
  --password <password>  Password/token passcode; visible in ps and shell history
  --password-stdin       Read the password from stdin (scripts/CI)
  --url <url>            Target address; defaults to NEXUS_URL, the current
                         context, or the build-time default
  --context <name>       Context name; defaults to the context matched by URL,
                         the current context, or "default"
  -o json, --json        JSON output

Examples:
  nexus-cli login --user ci-readonly
  nexus-cli login --url http://dev:8081 --user ci --context dev
  nexus-cli context use dev

About tokens:
  Nexus user tokens are a Pro-only feature (the Community edition has no such
  endpoint and returns 404). If you have a user token from a Pro instance or
  the web UI, pass it as --password: Nexus uses "<token name>:<passcode>" in
  place of "<user>:<password>" for Basic auth.
`

func loginUsage() string { return i18n.H("help.login", loginUsageEN) }

// Login stores and validates one context.
func Login(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		user      string
		pass      string
		passStdin bool
		out       outputFlags
	)
	fs.StringVar(&user, "user", "", "account")
	fs.StringVar(&pass, "password", "", "password or user token passcode")
	fs.BoolVar(&passStdin, "password-stdin", false, "read the password from stdin")
	out.addTo(fs)
	fs.Usage = func() { fmt.Fprint(os.Stderr, loginUsage()) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, loginUsage())
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%s", i18n.T("login: unexpected argument %q (see --help)", fs.Arg(0)))
	}
	if strings.TrimSpace(user) == "" {
		return errors.New(i18n.T("login: --user is required"))
	}

	if passStdin {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("%s: %w", i18n.T("read stdin"), err)
		}
		pass = strings.TrimRight(string(b), "\r\n")
	} else if pass == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New(i18n.T("login: non-interactive input, use --password-stdin (or --password)"))
		}
		fmt.Fprintf(os.Stderr, i18n.T("Password (or user token passcode) for %s: "), user)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("%s: %w", i18n.T("read password"), err)
		}
		pass = string(b)
	}
	if pass == "" {
		return errors.New(i18n.T("login: empty password, nothing written"))
	}

	r, err := resolveTarget()
	if err != nil {
		return err
	}

	// Validate before writing, so a bad credential never reaches disk and keeps
	// triggering 401s (and Nexus auth rate limiting).
	c, err := nexus.New(r.URL, user, pass)
	if err != nil {
		return err
	}
	res, err := c.Check(context.Background())
	if err != nil {
		return err
	}
	if !res.Valid() {
		return errors.New(nexus.ExplainCheck(res))
	}

	name := strings.TrimSpace(GlobalContext)
	if name == "" {
		name = r.Context
	}
	if name == "" {
		name = config.DefaultContext
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if prev := cfg.Contexts[name]; prev != nil && prev.URL != "" && normalize(prev.URL) != normalize(r.URL) {
		fmt.Fprintf(os.Stderr, "nexus-cli: %s\n",
			i18n.T("context %q address changed from %s to %s", name, prev.URL, r.URL))
	}
	cfg.Set(name, &config.Context{URL: r.URL, User: user, Password: pass})
	if cfg.Current == "" {
		cfg.Current = name
	}
	if err := config.Save(cfg); err != nil {
		return err
	}

	type result struct {
		Context    string `json:"context"`
		URL        string `json:"url"`
		User       string `json:"user"`
		ConfigPath string `json:"config_path"`
		Checked    string `json:"checked"`
		IsCurrent  bool   `json:"is_current"`
	}
	res2 := result{
		Context: name, URL: r.URL, User: user, ConfigPath: config.Path(),
		Checked: nexus.ExplainCheck(res), IsCurrent: cfg.Current == name,
	}
	return emit(out.JSON, res2, func(w io.Writer) {
		fmt.Fprintln(w, res2.Checked)
		fmt.Fprintf(w, "%s\n", i18n.T("context: %s%s", name, currentMark(res2.IsCurrent)))
		fmt.Fprintf(w, "%s\n", i18n.T("wrote %s (mode 0600)", res2.ConfigPath))
		fmt.Fprintf(w, "%s\n", i18n.T("target: %s", res2.URL))
		if !res2.IsCurrent {
			fmt.Fprintf(w, "%s\n", i18n.T("switch to it: nexus-cli context use %s", name))
		}
	})
}

const whoamiUsageEN = `Usage: nexus-cli whoami [--context <name>] [--url <url>] [--check] [-o json]

Shows the effective context, target address, and where each value comes from.

  --context <name>   Inspect a specific context
  --url <url>        Resolve against a specific address
  --check            Make one request to confirm the credentials work
`

func whoamiUsage() string { return i18n.H("help.whoami", whoamiUsageEN) }

// Whoami reports the effective context.
func Whoami(args []string) error {
	fs := flag.NewFlagSet("whoami", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		check bool
		out   outputFlags
	)
	fs.BoolVar(&check, "check", false, "verify connectivity")
	out.addTo(fs)
	fs.Usage = func() { fmt.Fprint(os.Stderr, whoamiUsage()) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, whoamiUsage())
			return nil
		}
		return err
	}

	r, err := resolveTarget()
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	info := struct {
		Context     string   `json:"context"`
		ContextFrom string   `json:"context_from,omitempty"`
		URL         string   `json:"url"`
		URLFrom     string   `json:"url_from"`
		User        string   `json:"user"`
		CredFrom    string   `json:"credential_from,omitempty"`
		PasswordSet bool     `json:"password_set"`
		ConfigPath  string   `json:"config_path"`
		Contexts    []string `json:"contexts"`
		Mismatch    string   `json:"mismatch,omitempty"`
		Check       string   `json:"check,omitempty"`
		Lang        string   `json:"lang"`
	}{
		Context:     r.Context,
		ContextFrom: r.ContextFrom,
		URL:         r.URL,
		URLFrom:     r.URLFrom,
		User:        redact(r.User),
		CredFrom:    r.CredFrom,
		PasswordSet: r.Pass != "",
		ConfigPath:  r.ConfigPath,
		Contexts:    cfg.Names(),
		Lang:        string(i18n.Current()),
	}
	// Warn when the target differs from what the context recorded, so credentials
	// for A are never silently sent to B.
	if r.ContextURL != "" && normalize(r.ContextURL) != normalize(r.URL) {
		info.Mismatch = i18n.T("context %q records %s but the target is %s", r.Context, r.ContextURL, r.URL)
	}

	if check {
		c, err := nexus.New(r.URL, r.User, r.Pass)
		if err != nil {
			return err
		}
		res, err := c.Check(context.Background())
		if err != nil {
			info.Check = i18n.T("failed: %s", err.Error())
		} else {
			info.Check = nexus.ExplainCheck(res)
		}
	}

	err = emit(out.JSON, info, func(w io.Writer) {
		ctx := r.Context
		if ctx == "" {
			ctx = i18n.T("<none>")
		}
		fmt.Fprintf(w, "%s\n", i18n.T("context: %s%s", ctx, from(r.ContextFrom)))
		fmt.Fprintf(w, "%s\n", i18n.T("address: %s%s", r.URL, from(r.URLFrom)))
		fmt.Fprintf(w, "%s\n", i18n.T("account: %s%s", orUnset(info.User), from(r.CredFrom)))
		if info.PasswordSet {
			fmt.Fprintln(w, i18n.T("password: set"))
		} else {
			fmt.Fprintln(w, i18n.T("password: <unset>"))
		}
		fmt.Fprintf(w, "%s\n", i18n.T("config file: %s", r.ConfigPath))
		if len(info.Contexts) > 0 {
			fmt.Fprintf(w, "%s\n", i18n.T("contexts: %s (current=%s)", strings.Join(info.Contexts, ", "), orUnset(cfg.Current)))
		}
		if info.Mismatch != "" {
			fmt.Fprintf(w, "⚠ %s\n", info.Mismatch)
		}
		if info.Check != "" {
			fmt.Fprintf(w, "%s\n", i18n.T("connectivity: %s", info.Check))
		}
	})
	if err != nil {
		return err
	}
	if info.Mismatch != "" {
		return errors.New(i18n.T("target address does not match the context record (see above)"))
	}
	if check && !strings.Contains(info.Check, i18n.T("valid")) {
		return errors.New(i18n.T("connectivity check failed"))
	}
	return nil
}

const contextUsageEN = `Usage: nexus-cli context <subcommand>

  context list              List all contexts (* marks the current one)
  context use <name>        Switch the current context
  context show [name]       Inspect a context (defaults to the current one)
  context remove <name>     Delete a context
  -o json, --json           JSON output
`

func contextUsage() string { return i18n.H("help.context", contextUsageEN) }

// ContextCmd implements the context subcommands.
func ContextCmd(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, contextUsage())
		return errors.New(i18n.T("context: a subcommand is required"))
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "-h", "--help", "help":
		fmt.Fprint(os.Stdout, contextUsage())
		return nil
	case "list", "ls":
		return contextList(rest)
	case "use":
		return contextUse(rest)
	case "show":
		return contextShow(rest)
	case "remove", "rm":
		return contextRemove(rest)
	default:
		return fmt.Errorf("%s", i18n.T("context: unknown subcommand %q (see --help)", sub))
	}
}

func contextList(args []string) error {
	fs := flag.NewFlagSet("context list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var out outputFlags
	out.addTo(fs)
	if err := fs.Parse(args); err != nil && !errors.Is(err, flag.ErrHelp) {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	type row struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		User    string `json:"user"`
		Current bool   `json:"current"`
	}
	rows := make([]row, 0, len(cfg.Contexts))
	for _, n := range cfg.Names() {
		c := cfg.Contexts[n]
		rows = append(rows, row{Name: n, URL: c.URL, User: c.User, Current: n == cfg.Current})
	}
	return emit(out.JSON, map[string]any{"current": cfg.Current, "contexts": rows}, func(w io.Writer) {
		if len(rows) == 0 {
			fmt.Fprintln(w, i18n.T("No contexts yet. Run `nexus-cli login --user <account>` to create one."))
			return
		}
		for _, r := range rows {
			mark := "  "
			if r.Current {
				mark = "* "
			}
			fmt.Fprintf(w, "%s%-12s %-32s %s\n", mark, r.Name, orUnset(r.URL), orUnset(r.User))
		}
	})
}

func contextUse(args []string) error {
	fs := flag.NewFlagSet("context use", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var out outputFlags
	out.addTo(fs)
	if err := fs.Parse(args); err != nil && !errors.Is(err, flag.ErrHelp) {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New(i18n.T("usage: nexus-cli context use <name>"))
	}
	name := fs.Arg(0)
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, ok := cfg.Contexts[name]
	if !ok {
		return fmt.Errorf("%s", i18n.T("context %q does not exist (existing: %s)", name, strings.Join(cfg.Names(), ", ")))
	}
	cfg.Current = name
	if err := config.Save(cfg); err != nil {
		return err
	}
	return emit(out.JSON, map[string]string{"current": name, "url": ctx.URL}, func(w io.Writer) {
		fmt.Fprintln(w, i18n.T("switched to context %q (%s)", name, orUnset(ctx.URL)))
	})
}

func contextShow(args []string) error {
	fs := flag.NewFlagSet("context show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var out outputFlags
	out.addTo(fs)
	if err := fs.Parse(args); err != nil && !errors.Is(err, flag.ErrHelp) {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	name := cfg.Current
	if fs.NArg() > 0 {
		name = fs.Arg(0)
	}
	if name == "" {
		return errors.New(i18n.T("no current context; run `nexus-cli login` first"))
	}
	c, ok := cfg.Contexts[name]
	if !ok {
		return fmt.Errorf("%s", i18n.T("context %q does not exist", name))
	}
	info := map[string]any{
		"name": name, "url": c.URL, "user": c.User,
		"password_set": c.Password != "", "is_current": cfg.Current == name,
	}
	return emit(out.JSON, info, func(w io.Writer) {
		mark := ""
		if cfg.Current == name {
			mark = i18n.T(" (current)")
		}
		fmt.Fprintf(w, "%s\n", i18n.T("context %s%s", name, mark))
		fmt.Fprintf(w, "  %s\n", i18n.T("address: %s", orUnset(c.URL)))
		fmt.Fprintf(w, "  %s\n", i18n.T("account: %s", orUnset(c.User)))
		if c.Password != "" {
			fmt.Fprintf(w, "  %s\n", i18n.T("password: set"))
		} else {
			fmt.Fprintf(w, "  %s\n", i18n.T("password: <unset>"))
		}
	})
}

func contextRemove(args []string) error {
	if len(args) != 1 {
		return errors.New(i18n.T("usage: nexus-cli context remove <name>"))
	}
	name := args[0]
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	wasCurrent := cfg.Current == name
	if !cfg.Delete(name) {
		return fmt.Errorf("%s", i18n.T("context %q does not exist", name))
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	if wasCurrent {
		fmt.Println(i18n.T("removed context %q (it was current; current is now %s)", name, orUnset(cfg.Current)))
	} else {
		fmt.Println(i18n.T("removed context %q", name))
	}
	return nil
}

func normalize(u string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(u), "/"))
}

func currentMark(isCurrent bool) string {
	if isCurrent {
		return i18n.T(" (current)")
	}
	return ""
}

// from renders where a value came from, using the translated source labels.
func from(src string) string {
	if src == "" {
		return ""
	}
	return i18n.T(" (from %s)", i18n.T(src))
}

func redact(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= 4 {
		return strings.Repeat("*", len(r))
	}
	return string(r[:4]) + "…"
}

func orUnset(s string) string {
	if s == "" {
		return i18n.T("<unset>")
	}
	return s
}
