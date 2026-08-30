package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/zainfathoni/amux/internal/result"
)

var (
	version = "dev"
	commit  = ""
	built   = ""
)

type options struct {
	dryRun bool
}

type attachMode int

const (
	attachAuto attachMode = iota
	attachAlways
	attachNever
)

type app struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func main() {
	a := app{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}
	if err := a.execute(os.Args[1:]); err != nil {
		fmt.Fprintln(a.stderr, err)
		os.Exit(result.ExitCode(err))
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func normalizedTmuxStartCommand(startCommand string) string {
	if strings.HasPrefix(startCommand, "\"") && strings.HasSuffix(startCommand, "\"") {
		if unquoted, err := strconv.Unquote(startCommand); err == nil {
			return unquoted
		}
	}
	return startCommand
}

func versionString() string {
	parts := []string{"amux", version}
	if commit != "" {
		parts = append(parts, "commit="+commit)
	}
	if built != "" {
		parts = append(parts, "built="+built)
	}
	return strings.Join(parts, " ")
}
