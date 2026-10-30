package main

import (
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// options holds the values gathered from the command line.
type options struct {
	program    string
	board      string
	url        string
	pin        string
	configPath string
	output     string
	headers    headerFlags
	files      stringList
	timeout    time.Duration
	retry      int
	json       bool
	quiet      bool
	noConfig   bool
	appendMode bool
	diff       bool
	stat       bool
	dryRun     bool
	force      bool
	paths      bool
}

// stringList collects a flag that may be repeated, such as -f.
type stringList []string

// String renders the collected values for the flag package.
func (l *stringList) String() string {
	return strings.Join(*l, ", ")
}

// Set appends one value.
func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

// headerFlags collects repeated --header "Name: Value" values.
type headerFlags struct {
	values http.Header
}

// String renders the collected header names for the flag package.
func (h headerFlags) String() string {
	if len(h.values) == 0 {
		return ""
	}

	names := make([]string, 0, len(h.values))
	for name := range h.values {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// Set parses one "Name: Value" header.
func (h *headerFlags) Set(value string) error {
	name, raw, found := strings.Cut(value, ":")
	name = strings.TrimSpace(name)
	if !found || name == "" {
		return fmt.Errorf("header %q must look like \"Name: Value\"", value)
	}

	if h.values == nil {
		h.values = make(http.Header)
	}
	h.values.Add(name, strings.TrimSpace(raw))
	return nil
}

// registerSharedFlags adds the flags every command accepts.
func registerSharedFlags(flags *flag.FlagSet, opts *options) {
	flags.StringVar(&opts.board, "b", opts.board, "board to use")
	flags.StringVar(&opts.board, "board", opts.board, "board to use")
	flags.StringVar(&opts.url, "u", opts.url, "GoSpin server URL")
	flags.StringVar(&opts.url, "url", opts.url, "GoSpin server URL")
	flags.StringVar(&opts.pin, "pin", opts.pin, "PIN of a board protected by ghostboard.private_boards")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "request timeout")
	flags.StringVar(&opts.configPath, "config", opts.configPath, "configuration file to use")
	flags.BoolVar(&opts.noConfig, "no-config", false, "ignore the configuration files")
	flags.BoolVar(&opts.quiet, "q", false, "suppress confirmations")
	flags.BoolVar(&opts.quiet, "quiet", false, "suppress confirmations")
	flags.BoolVar(&opts.json, "json", false, "write a machine-readable result on stdout")
	flags.IntVar(&opts.retry, "retry", 0, "retry a transient failure this many times")
	flags.Var(&opts.headers, "header", "extra HTTP header (Name: Value), repeated")
}

// registerCommandFlags adds the flags of one command.
func registerCommandFlags(flags *flag.FlagSet, opts *options, command string) {
	switch command {
	case "add", "set":
		flags.Var(&opts.files, "f", "file to read the text from, repeated")
		flags.BoolVar(&opts.dryRun, "dry-run", false, "show what would change without writing")
	case "upload":
		flags.BoolVar(&opts.appendMode, "append", false, "append instead of replacing")
		flags.BoolVar(&opts.dryRun, "dry-run", false, "show what would change without writing")
	case "download":
		flags.StringVar(&opts.output, "o", "", "output file")
		flags.BoolVar(&opts.force, "force", false, "overwrite an existing file")
	case "read":
		flags.BoolVar(&opts.stat, "stat", false, "print revision and update time on stderr")
	case "follow":
		flags.BoolVar(&opts.diff, "diff", false, "print only what a revision added")
	case "config":
		flags.BoolVar(&opts.paths, "paths", false, "list the configuration files looked for")
	case "file":
		flags.StringVar(&opts.output, "o", "", "output file (file get)")
		flags.BoolVar(&opts.force, "force", false, "overwrite an existing file (file get)")
	}
}

// parseFlags parses the flags and returns the positional arguments. The
// standard flag package stops at the first positional argument, which would
// silently turn a flag written after it into text, so parsing is repeated on
// the remainder until every argument is consumed.
func parseFlags(flags *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	rest := args
	for {
		if err := flags.Parse(rest); err != nil {
			return nil, err
		}
		remaining := flags.Args()
		if len(remaining) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, remaining[0])
		rest = remaining[1:]
	}
}

// splitCommand finds the subcommand and returns it with every other argument.
// Flags before the command are kept, so both "gb-cli -b x add text" and
// "gb-cli add -b x text" are accepted.
func splitCommand(args []string) (string, []string, error) {
	rest := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if isCommand(arg) {
			rest = append(rest, args[i+1:]...)
			return arg, rest, nil
		}

		if flagTakesValue(arg) {
			rest = append(rest, arg)
			if i+1 < len(args) {
				rest = append(rest, args[i+1])
				i++
			}
			continue
		}

		if strings.HasPrefix(arg, "-") {
			rest = append(rest, arg)
			continue
		}

		return "", nil, fmt.Errorf("unknown command %q", arg)
	}

	return "", nil, nil
}

// isCommand reports whether token is one of the subcommands.
func isCommand(token string) bool {
	switch token {
	case "add", "set", "read", "upload", "download", "follow", "edit", "file", "config", "completion", "version", "pin-hash":
		return true
	default:
		return false
	}
}

// flagTakesValue reports whether arg is a flag whose value is the next
// argument, so the value is not mistaken for the subcommand.
func flagTakesValue(arg string) bool {
	switch arg {
	case "-b", "--board", "-u", "--url", "--pin", "--timeout", "--config", "--retry", "--header", "-f", "-o":
		return true
	default:
		return false
	}
}

// prescanFlag reads a string flag before the command flags are parsed, so the
// configuration file is known before its values become flag defaults. It stops
// at the `--` terminator and keeps the last occurrence, exactly like the flag
// package, so a text that starts with "-" is never read as a flag.
func prescanFlag(args []string, name string) (string, error) {
	long := "--" + name
	short := "-" + name

	value := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return value, nil
		}

		switch {
		case arg == long || arg == short:
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s needs a value", arg)
			}
			value = args[i+1]
			i++
		case strings.HasPrefix(arg, long+"="):
			value = strings.TrimPrefix(arg, long+"=")
		case strings.HasPrefix(arg, short+"="):
			value = strings.TrimPrefix(arg, short+"=")
		}
	}
	return value, nil
}

// prescanBool reports whether a boolean flag is present, so a flag that changes
// how the configuration is loaded can be honoured before parsing. It stops at
// the `--` terminator, where every following argument is text.
func prescanBool(args []string, name string) bool {
	long := "--" + name
	short := "-" + name

	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == long || arg == short {
			return true
		}
	}
	return false
}

// isHelp reports whether the invocation asks for the usage text.
func isHelp(args []string) bool {
	switch args[0] {
	case "-h", "--help", "-help", "help":
		return true
	default:
		return false
	}
}

// isVersionArg reports whether the invocation is the version flag.
func isVersionArg(arg string) bool {
	switch arg {
	case "-version", "--version", "-V":
		return true
	default:
		return false
	}
}
