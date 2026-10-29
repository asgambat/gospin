package main

import (
	"fmt"
	"io"
	"strings"
)

const (
	// completionCommands are the subcommands offered by every generated script.
	completionCommands = "add set read upload download follow edit config pin-hash completion version help"

	// completionFlags are the flags offered by every generated script.
	completionFlags = "-b --board -u --url --pin --timeout --config --no-config --retry --header -q --quiet --json -f --append -o --force --stat --diff --dry-run --paths -h --help"
)

// execCompletion prints a completion script for the requested shell. It runs
// before the configuration is validated, so completing works even while the URL
// is being fixed.
func execCompletion(opts options, positionals []string, stdout, stderr io.Writer) int {
	if len(positionals) != 1 {
		return usageError(opts.program, stderr, "completion", "needs a shell: bash, zsh or fish")
	}

	var script string
	switch positionals[0] {
	case "bash":
		script = bashCompletion(opts.program)
	case "zsh":
		script = zshCompletion(opts.program)
	case "fish":
		script = fishCompletion(opts.program)
	default:
		return usageError(opts.program, stderr, "completion", "unsupported shell %q: use bash, zsh or fish", positionals[0])
	}

	writeMessage(stdout, "%s", script)
	return exitOK
}

// bashCompletion renders a bash completion script for the program.
func bashCompletion(program string) string {
	function := "complete_" + sanitizeFunctionName(program)

	return fmt.Sprintf(`# bash completion for %[1]s
%[2]s() {
    local commands="%[3]s"
    local flags="%[4]s"
    local current="${COMP_WORDS[COMP_CWORD]}"

    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "$commands $flags" -- "$current") )
        return 0
    fi

    COMPREPLY=( $(compgen -W "$flags" -- "$current") )
}

complete -F %[2]s %[1]s
`, program, function, completionCommands, completionFlags)
}

// zshCompletion renders a zsh completion script for the program.
func zshCompletion(program string) string {
	return fmt.Sprintf(`#compdef %[1]s
# zsh completion for %[1]s

_%[2]s() {
    local commands=(%[3]s)
    local flags=(%[4]s)

    if (( CURRENT == 2 )); then
        _describe 'command' commands
        return
    fi

    _describe 'flag' flags
}

compdef _%[2]s %[1]s
`, program, sanitizeFunctionName(program), completionCommands, completionFlags)
}

// fishCompletion renders a fish completion script for the program.
func fishCompletion(program string) string {
	return fmt.Sprintf(`# fish completion for %[1]s
complete -c %[1]s -f -n '__fish_use_subcommand' -a '%[2]s'
complete -c %[1]s -l board -s b -d 'board to use'
complete -c %[1]s -l url -s u -d 'GoSpin server URL'
complete -c %[1]s -l pin -d 'PIN of a protected board'
complete -c %[1]s -l timeout -d 'request timeout'
complete -c %[1]s -l config -d 'configuration file'
complete -c %[1]s -l no-config -d 'ignore the configuration files'
complete -c %[1]s -l retry -d 'retry a transient failure'
complete -c %[1]s -l header -d 'extra HTTP header'
complete -c %[1]s -l quiet -s q -d 'suppress confirmations'
complete -c %[1]s -l json -d 'machine-readable output'
complete -c %[1]s -s f -d 'file to read the text from'
complete -c %[1]s -l dry-run -d 'show what would change without writing'
complete -c %[1]s -l append -d 'append instead of replacing'
complete -c %[1]s -s o -d 'output file'
complete -c %[1]s -l force -d 'overwrite an existing file'
complete -c %[1]s -l stat -d 'print the revision and the update time'
complete -c %[1]s -l diff -d 'print only what a revision added'
complete -c %[1]s -l paths -d 'list the configuration files looked for'
complete -c %[1]s -l version -d 'print the build metadata'
complete -c %[1]s -l help -s h -d 'show help'
`, program, completionCommands)
}

// sanitizeFunctionName turns a program name into a valid shell function name.
func sanitizeFunctionName(program string) string {
	var builder strings.Builder
	for _, char := range program {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
			builder.WriteRune(char)
		default:
			builder.WriteRune('_')
		}
	}

	name := builder.String()
	if name == "" {
		return defaultProgramName
	}
	return name
}
