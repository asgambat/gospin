// Command board is the gb-cli binary: it reads and writes the shared text
// boards (ghostboards) served by a running GoSpin instance.
//
// The CLI is a thin client of the plain-text board API, so it works against a
// local server, a container or a remote host without touching any file of the
// server. The help and the error messages always name the executable as it was
// invoked, so a renamed binary documents itself.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bassista/go_spin/internal/boardclient"
	"github.com/bassista/go_spin/internal/version"
)

const (
	// Exit codes of the CLI. The first three follow the usual convention; the
	// last two let a script tell a wrong URL and a wrong board name from a
	// generic failure.
	exitOK        = 0
	exitError     = 1
	exitUsage     = 2
	exitEndpoint  = 3
	exitBoardName = 4
)

// defaultProgramName is the binary name used when the invoked name cannot be
// determined. It is also the name the build targets produce.
const defaultProgramName = "gb-cli"

// programPlaceholder is replaced with the invoked program name in the help
// text, so the help always matches the name of the executable.
const programPlaceholder = "{program}"

func main() {
	os.Exit(run(programName(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// programName is the name the binary was invoked as, so the help and the error
// messages follow a renamed executable.
func programName() string {
	if len(os.Args) == 0 || os.Args[0] == "" {
		return defaultProgramName
	}

	name := filepath.Base(os.Args[0])
	if name == "" || name == "." || name == string(filepath.Separator) {
		return defaultProgramName
	}
	return strings.TrimSuffix(name, ".exe")
}

// run executes one CLI invocation and returns its exit code. It is separated
// from main so the whole surface can be exercised by tests; program is the name
// the executable was invoked as.
func run(program string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if program == "" {
		program = defaultProgramName
	}

	if len(args) == 0 {
		writeMessage(stderr, "%s: no command given\n\n", program)
		printUsage(stderr, program)
		return exitUsage
	}
	if isHelp(args) {
		printUsage(stdout, program)
		return exitOK
	}
	if isVersionArg(args[0]) {
		return execVersion(options{program: program}, nil, stdout, stderr)
	}

	command, rest, err := splitCommand(args)
	if err != nil {
		writeMessage(stderr, "%s: %v\n\n", program, err)
		printUsage(stderr, program)
		return exitUsage
	}
	if command == "" {
		writeMessage(stderr, "%s: no command given\n\n", program)
		printUsage(stderr, program)
		return exitUsage
	}

	// The configuration file is resolved before the flags are parsed, because
	// the flag defaults come from it; --config and --no-config are therefore
	// pre-scanned.
	noConfig := prescanBool(args, "no-config")
	configPath := ""
	if !noConfig {
		if configPath, err = prescanFlag(args, "config"); err != nil {
			writeMessage(stderr, "%s: %v\n", program, err)
			return exitUsage
		}
	}

	loadOptions := boardclient.LoadOptions{
		HomeDir:        homeDir(),
		ExecutablePath: executablePath(),
		XDGConfigHome:  os.Getenv("XDG_CONFIG_HOME"),
		Getenv:         os.Getenv,
		ConfigPath:     configPath,
		SkipFile:       noConfig,
	}

	cfg, err := boardclient.Load(loadOptions)
	if err != nil {
		return fail(program, stderr, err)
	}

	opts := options{program: program, board: cfg.Board, url: cfg.URL, timeout: cfg.Timeout, configPath: configPath}

	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	registerSharedFlags(flags, &opts)
	registerCommandFlags(flags, &opts, command)

	positionals, err := parseFlags(flags, rest)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout, program)
			return exitOK
		}
		writeMessage(stderr, "%s %s: %v\n", program, command, err)
		return exitUsage
	}

	// A value given on the command line is a misuse of the command line, not a
	// failure of the server or of the configuration file, so it exits with the
	// code scripts use to tell the two apart.
	if opts.retry < 0 {
		return usageError(program, stderr, command, "--retry must not be negative")
	}
	if opts.timeout <= 0 && flagWasSet(flags, "timeout") {
		return usageError(program, stderr, command, "--timeout must be positive")
	}

	// The commands that only report things run before the configuration is
	// validated: they must keep working while a value is being fixed.
	switch command {
	case "version":
		return execVersion(opts, positionals, stdout, stderr)
	case "config":
		return execConfig(opts, cfg, boardclient.ConfigPaths(loadOptions), positionals, stdout, stderr)
	case "completion":
		return execCompletion(opts, positionals, stdout, stderr)
	}

	// The effective configuration is validated only now, after the flags: a
	// value from the configuration file is a default that a flag may correct.
	if err := (boardclient.Config{URL: opts.url, Board: opts.board, Timeout: opts.timeout}).Validate(); err != nil {
		return fail(program, stderr, err)
	}

	client, err := boardclient.NewWithOptions(opts.url, boardclient.Options{
		Timeout: opts.timeout,
		Retries: opts.retry,
		Headers: requestHeaders(opts),
	})
	if err != nil {
		return fail(program, stderr, err)
	}

	ctx := context.Background()
	switch command {
	case "add":
		return execAdd(ctx, client, opts, positionals, stdin, stdout, stderr)
	case "set":
		return execSet(ctx, client, opts, positionals, stdin, stdout, stderr)
	case "read":
		return execRead(ctx, client, opts, positionals, stdout, stderr)
	case "upload":
		return execUpload(ctx, client, opts, positionals, stdin, stdout, stderr)
	case "download":
		return execDownload(ctx, client, opts, positionals, stdout, stderr)
	case "follow":
		return execFollow(ctx, client, opts, positionals, stdout, stderr)
	case "edit":
		return execEdit(ctx, client, opts, positionals, stdin, stdout, stderr)
	default:
		// splitCommand only returns the commands listed above.
		return usageError(program, stderr, command, "unknown command")
	}
}

// requestHeaders merges --header with the token environment variable, so a
// board behind an authenticating proxy works without repeating the flag.
func requestHeaders(opts options) http.Header {
	headers := opts.headers.values
	if headers == nil {
		headers = make(http.Header)
	}
	if headers.Get("Authorization") != "" {
		return headers
	}
	if token := strings.TrimSpace(os.Getenv("GO_SPIN_BOARD_TOKEN")); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	return headers
}

// versionReport is the machine-readable form of the version command.
type versionReport struct {
	Program   string `json:"program"`
	Version   string `json:"version"`
	BuildTime string `json:"build_time"`
	GitCommit string `json:"git_commit"`
	GoVersion string `json:"go_version"`
}

// execVersion prints the build metadata the CLI was compiled with.
func execVersion(opts options, positionals []string, stdout, stderr io.Writer) int {
	if len(positionals) > 0 {
		return usageError(opts.program, stderr, "version", "unexpected argument %q", positionals[0])
	}

	if opts.json {
		report := versionReport{
			Program:   opts.program,
			Version:   version.Version,
			BuildTime: version.BuildTime,
			GitCommit: version.GitCommit,
			GoVersion: version.GoVersion,
		}
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			return fail(opts.program, stderr, fmt.Errorf("writing the build metadata: %w", err))
		}
		return exitOK
	}

	writeMessage(stdout, "%s %s\n", opts.program, version.BuildInfo())
	return exitOK
}

// misuseError marks a failure caused by how the command line was written, so
// it exits with the misuse code instead of the generic error code.
type misuseError struct {
	err error
}

// Error renders the underlying failure.
func (e *misuseError) Error() string {
	return e.err.Error()
}

// Unwrap exposes the underlying failure to errors.Is and errors.As.
func (e *misuseError) Unwrap() error {
	return e.err
}

// misuse marks err as a misuse of the command line.
func misuse(err error) error {
	if err == nil {
		return nil
	}
	return &misuseError{err: err}
}

// fail reports a runtime failure and maps the typed failures to their exit
// code so scripts can tell them apart.
func fail(program string, stderr io.Writer, err error) int {
	writeMessage(stderr, "%s: %v\n", program, err)

	var endpointErr *boardclient.EndpointError
	if errors.As(err, &endpointErr) {
		return exitEndpoint
	}
	var boardNameErr *boardclient.BoardNameError
	if errors.As(err, &boardNameErr) {
		return exitBoardName
	}
	var misuseErr *misuseError
	if errors.As(err, &misuseErr) {
		return exitUsage
	}
	return exitError
}

// flagWasSet reports whether a flag was given on the command line, so a value
// that is invalid only fails as a misuse when the user actually wrote it: the
// same value coming from the configuration file is a configuration error.
func flagWasSet(flags *flag.FlagSet, name string) bool {
	found := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// usageError reports a misuse of a command.
func usageError(program string, stderr io.Writer, command, format string, args ...any) int {
	writeMessage(stderr, "%s %s: %s\n", program, command, fmt.Sprintf(format, args...))
	return exitUsage
}

// homeDir returns the user's home directory, or an empty string when it cannot
// be determined.
func homeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return dir
}

// executablePath returns the path of the running binary, or an empty string
// when it cannot be determined.
func executablePath() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	return path
}
