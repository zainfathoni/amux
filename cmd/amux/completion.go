package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

type completionCommand struct {
	Name        string
	Description string
	Flags       []string
	Args        string
	Subcommands []completionCommand
}

var completionCommands = []completionCommand{
	{
		Name:        "worker",
		Description: "Manage interactive thread-bound clients",
		Subcommands: []completionCommand{
			{Name: "list", Description: "List configured workers", Flags: []string{"--workspace", "--thread", "--shelf", "--current", "--all", "-w", "-t"}},
			{Name: "pin", Description: "Pin a worker", Flags: []string{"--workspace", "--window", "--workdir", "--thread", "--current", "-w", "-W", "-d", "-t"}},
			{Name: "unpin", Description: "Unpin a worker", Flags: []string{"--thread", "--current", "-t"}},
			{Name: "launch", Description: "Launch workers", Flags: []string{"--workspace", "--thread", "--current", "--all", "-w", "-t"}},
			{Name: "park", Description: "Park workers", Flags: []string{"--workspace", "--thread", "--current", "--all", "-w", "-t"}},
			{Name: "restart", Description: "Restart workers", Flags: []string{"--workspace", "--thread", "--current", "--all", "-w", "-t"}},
			{Name: "remove", Description: "Remove workers", Flags: []string{"--workspace", "--thread", "--current", "--all", "-w", "-t"}},
			{Name: "spawn", Description: "Spawn a worker", Flags: []string{"--workspace", "--window", "--workdir", "--mode", "-m", "--title-prefix", "--group", "--message", "--message-file", "--message-stdin", "--idempotency-key", "-w", "-W", "-d"}},
			{Name: "shelve", Description: "Shelve workers", Flags: []string{"--workspace", "--thread", "--current", "--all", "-w", "-t"}},
			{Name: "unshelve", Description: "Unshelve workers", Flags: []string{"--workspace", "--thread", "--current", "--all", "-w", "-t"}},
			{Name: "teardown", Description: "Teardown workers", Flags: []string{"--workspace", "--thread", "--current", "--all", "-w", "-t"}},
			{Name: "doctor", Description: "Diagnose workers", Flags: []string{"--workspace", "--thread", "--current", "--all", "-w", "-t"}},
			{Name: "reconcile", Description: "Reconcile workers", Flags: []string{"--workspace", "--thread", "--current", "--all", "-w", "-t"}},
		},
	},
	{Name: "spawn", Description: "Spawn a worker", Flags: []string{"--workspace", "--window", "--workdir", "--mode", "-m", "--title-prefix", "--group", "--message", "--message-file", "--message-stdin", "--idempotency-key", "-w", "-W", "-d"}},
	{Name: "shelve", Description: "Shelve workers", Flags: []string{"--workspace", "--thread", "--current", "--all", "-w", "-t"}},
	{Name: "unshelve", Description: "Unshelve workers", Flags: []string{"--workspace", "--thread", "--current", "--all", "-w", "-t"}},
	{Name: "teardown", Description: "Teardown workers", Flags: []string{"--workspace", "--thread", "--current", "--all", "-w", "-t"}},
	{Name: "group", Description: "Manage durable Amp thread groups", Subcommands: []completionCommand{
		{Name: "declare", Description: "Declare a group with its coordinator", Flags: []string{"--group", "--thread", "-t"}},
		{Name: "add", Description: "Add explicit group membership", Flags: []string{"--group", "--thread", "-t"}},
		{Name: "remove", Description: "Remove local group membership", Flags: []string{"--group", "--thread", "-t"}},
		{Name: "coordinator", Description: "Designate a group coordinator", Flags: []string{"--group", "--thread", "-t"}},
		{Name: "list", Description: "List durable group memberships", Flags: []string{"--group", "--thread", "--all", "-t"}},
		{Name: "show", Description: "Show one durable group", Flags: []string{"--group"}},
		{Name: "reconcile", Description: "Add-only ensure group labels", Flags: []string{"--group", "--thread", "--all", "-t"}},
	}},
	{Name: "callback", Description: "Manage ephemeral coordinator callback leases", Subcommands: []completionCommand{
		{Name: "register", Description: "Register an exact coordinator pane", Flags: []string{"--group", "--thread", "--pane", "-t"}},
		{Name: "clear", Description: "Invalidate a coordinator callback lease", Flags: []string{"--group"}},
	}},
	{Name: "report", Description: "Manage durable worker reports and finish authorization", Subcommands: []completionCommand{
		{Name: "submit", Description: "Submit or progress a durable report", Flags: []string{"--report-id", "--group", "--thread", "--status", "--issue", "--reference", "--pr", "--summary", "-t"}},
		{Name: "pending", Description: "List unacknowledged reports", Flags: []string{"--group", "--thread", "--all", "-t"}},
		{Name: "history", Description: "Show report history", Flags: []string{"--report-id"}},
		{Name: "acknowledge", Description: "Acknowledge a report", Flags: []string{"--report-id"}},
		{Name: "authorize-finish", Description: "Authorize finish for a ready report", Flags: []string{"--report-id", "--thread", "--reference", "-t"}},
	}},
	{Name: "install", Description: "Inspect the amux client installation", Subcommands: []completionCommand{
		{Name: "doctor", Description: "Diagnose executable targets, versions, and PATH drift"},
	}},
	{Name: "migrate-config", Description: "Copy legacy config files into ~/.config/amux"},
	{Name: "completion", Description: "Print shell completion script", Args: "<bash|zsh|fish>"},
	{Name: "update", Description: "Update a user-local amux release install"},
	{Name: "version", Description: "Print version and build metadata"},
	{Name: "path", Description: "Print the config directory"},
	{Name: "help", Description: "Print help"},
}

var globalCompletionFlags = []string{"--config-dir", "--json", "--dry-run", "--help", "-h", "--version", "-c", "-j", "-n"}

const ampDialModeCompletions = "low medium high ultra"

func (a app) completion(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: amux completion <bash|zsh|fish>")
	}
	switch args[0] {
	case "bash":
		writeBashCompletion(a.stdout)
	case "zsh":
		writeZshCompletion(a.stdout)
	case "fish":
		writeFishCompletion(a.stdout)
	default:
		return fmt.Errorf("unsupported shell %q; expected bash, zsh, or fish", args[0])
	}
	return nil
}

func writeBashCompletion(w io.Writer) {
	fmt.Fprintf(w, `# bash completion for amux; generated by amux completion bash
_amux_complete() {
  local cur command leaf i word
  cur="${COMP_WORDS[COMP_CWORD]}"
  for ((i=1; i<COMP_CWORD; i++)); do
    word="${COMP_WORDS[i]}"
    if [[ "$word" == --config-dir || "$word" == -c ]]; then ((i++)); continue; fi
    if [[ "$word" == --config-dir=* || "$word" == -c=* || "$word" == --json || "$word" == -j || "$word" == --dry-run || "$word" == -n ]]; then continue; fi
    if [[ -z "$command" ]]; then command="$word"; elif [[ ( "$command" == worker || "$command" == group || "$command" == callback || "$command" == report || "$command" == install ) && -z "$leaf" ]]; then leaf="$word"; fi
  done
  if [[ -z "$command" ]]; then
    COMPREPLY=( $(compgen -W "%s %s" -- "$cur") )
    return 0
  fi
  case "$command" in
    worker)
      if [[ -z "$leaf" ]]; then
        COMPREPLY=( $(compgen -W "%s" -- "$cur") )
      else
        case "$leaf" in
          spawn) COMPREPLY=( $(compgen -W "--workspace --window --workdir --mode -m --title-prefix --group --message --message-file --message-stdin --idempotency-key -w -W -d" -- "$cur") ) ;;
          pin) COMPREPLY=( $(compgen -W "--workspace --window --workdir --thread --current -w -W -d -t" -- "$cur") ) ;;
          unpin) COMPREPLY=( $(compgen -W "--thread --current -t" -- "$cur") ) ;;
          list) COMPREPLY=( $(compgen -W "--workspace --thread --shelf --current --all -w -t" -- "$cur") ) ;;
          *) COMPREPLY=( $(compgen -W "--workspace --thread --current --all -w -t" -- "$cur") ) ;;
        esac
      fi
      ;;
    install)
      if [[ -z "$leaf" ]]; then COMPREPLY=( $(compgen -W "doctor" -- "$cur") ); fi
      ;;
    group)
      if [[ -z "$leaf" ]]; then
        COMPREPLY=( $(compgen -W "declare add remove coordinator list show reconcile" -- "$cur") )
      else
        case "$leaf" in
          show) COMPREPLY=( $(compgen -W "--group" -- "$cur") ) ;;
          list|reconcile) COMPREPLY=( $(compgen -W "--group --thread --all -t" -- "$cur") ) ;;
          *) COMPREPLY=( $(compgen -W "--group --thread -t" -- "$cur") ) ;;
        esac
      fi
      ;;
    callback)
      if [[ -z "$leaf" ]]; then
        COMPREPLY=( $(compgen -W "register clear" -- "$cur") )
      elif [[ "$leaf" == register ]]; then
        COMPREPLY=( $(compgen -W "--group --thread --pane -t" -- "$cur") )
      else
        COMPREPLY=( $(compgen -W "--group" -- "$cur") )
      fi
      ;;
    report)
      if [[ -z "$leaf" ]]; then
        COMPREPLY=( $(compgen -W "submit pending history acknowledge authorize-finish" -- "$cur") )
      else
        case "$leaf" in
          submit) COMPREPLY=( $(compgen -W "--report-id --group --thread --status --issue --reference --pr --summary -t" -- "$cur") ) ;;
          pending) COMPREPLY=( $(compgen -W "--group --thread --all -t" -- "$cur") ) ;;
          authorize-finish) COMPREPLY=( $(compgen -W "--report-id --thread --reference -t" -- "$cur") ) ;;
          *) COMPREPLY=( $(compgen -W "--report-id" -- "$cur") ) ;;
        esac
      fi
      ;;
    spawn) COMPREPLY=( $(compgen -W "--workspace --window --workdir --mode -m --title-prefix --group --message --message-file --message-stdin --idempotency-key -w -W -d" -- "$cur") ) ;;
    shelve|unshelve|teardown) COMPREPLY=( $(compgen -W "--workspace --thread --current --all -w -t" -- "$cur") ) ;;
    completion) COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") ) ;;
  esac
}
complete -F _amux_complete amux
`, strings.Join(commandNames(completionCommands), " "), strings.Join(globalCompletionFlags, " "), strings.Join(commandNames(completionCommands[0].Subcommands), " "))
}

func writeZshCompletion(w io.Writer) {
	fmt.Fprintln(w, "#compdef amux")
	fmt.Fprintln(w, "# zsh completion for amux; generated by amux completion zsh")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "local -a commands")
	fmt.Fprintln(w, "commands=(")
	for _, command := range completionCommands {
		fmt.Fprintf(w, "  %q\n", command.Name+":"+command.Description)
	}
	fmt.Fprintln(w, ")")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "local -a worker_commands")
	fmt.Fprintln(w, "worker_commands=(")
	for _, command := range completionCommands[0].Subcommands {
		fmt.Fprintf(w, "  %q\n", command.Name+":"+command.Description)
	}
	fmt.Fprintln(w, ")")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "local -a install_commands")
	fmt.Fprintln(w, "install_commands=(\n  \"doctor:Diagnose executable targets, versions, and PATH drift\"\n)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "local -a group_commands")
	fmt.Fprintln(w, "group_commands=(")
	for _, command := range groupCompletionCommands() {
		fmt.Fprintf(w, "  %q\n", command.Name+":"+command.Description)
	}
	fmt.Fprintln(w, ")")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "local -a report_commands")
	fmt.Fprintln(w, "report_commands=(")
	for _, command := range reportCompletionCommands() {
		fmt.Fprintf(w, "  %q\n", command.Name+":"+command.Description)
	}
	fmt.Fprintln(w, ")")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "local -a callback_commands")
	fmt.Fprintln(w, "callback_commands=(")
	for _, command := range callbackCompletionCommands() {
		fmt.Fprintf(w, "  %q\n", command.Name+":"+command.Description)
	}
	fmt.Fprintln(w, ")")
	fmt.Fprintln(w)
	fmt.Fprintln(w, `local command leaf i word
i=2
while (( i <= CURRENT )); do
  word=$words[$i]
  case $word in
    --config-dir|-c) (( i += 2 )); continue ;;
    --config-dir=*|-c=*|--json|-j|--dry-run|-n) (( i++ )); continue ;;
    *) command=$word; (( i++ )); if [[ $command == worker || $command == group || $command == callback || $command == report || $command == install ]]; then leaf=$words[$i]; fi; break ;;
  esac
done`)
	fmt.Fprintln(w)
	fmt.Fprintln(w, `_arguments -C \
  '--config-dir[path to config directory]:directory:_directories' \
  '-c[path to config directory]:directory:_directories' \
  '--json[emit exactly one JSON result document]' \
  '-j[emit exactly one JSON result document]' \
  '--dry-run[print intended actions without mutating]' \
  '-n[print intended actions without mutating]' \
  '--help[print help]' \
  '-h[print help]' \
  '--version[print version]' \
  '1:command:->command' \
  '*::arg:->args'

case $state in
  command)
    _describe -t commands 'amux command' commands
    ;;
  args)
    case $command in
      worker)
        if [[ -z $leaf ]]; then
          _describe -t worker-commands 'worker command' worker_commands
        else
          case $leaf in
            spawn) _arguments '--workspace[workspace]:workspace:' '--window[window]:window:' '--workdir[working directory]:directory:_directories' '--mode[thread mode]:mode:(low medium high ultra)' '-m[thread mode]:mode:(low medium high ultra)' '--title-prefix[window and thread title prefix]:prefix:' '*--group[durable group id]:group:' '--message[initial message]:message:' '--message-file[read initial message from file]:message file:_files' '--message-stdin[read initial message from stdin]' '--idempotency-key[operation key]:key:' '-w[workspace]:workspace:' '-W[window]:window:' '-d[working directory]:directory:_directories' ;;
            pin) _arguments '--workspace[workspace]:workspace:' '--window[window]:window:' '--workdir[working directory]:directory:_directories' '--thread[thread id or URL]:thread:' '--current[current worker]' '-w[workspace]:workspace:' '-W[window]:window:' '-d[working directory]:directory:_directories' '-t[thread id or URL]:thread:' ;;
            unpin) _arguments '--thread[thread id or URL]:thread:' '--current[current worker]' '-t[thread id or URL]:thread:' ;;
            list) _arguments '--workspace[workspace]:workspace:' '--thread[thread id or URL]:thread:' '--shelf[shelf intent]:intent:(shelved unshelved)' '--current[current worker]' '--all[all workers]' '-w[workspace]:workspace:' '-t[thread id or URL]:thread:' ;;
            *) _arguments '--workspace[workspace]:workspace:' '--thread[thread id or URL]:thread:' '--current[current worker]' '--all[all workers]' '-w[workspace]:workspace:' '-t[thread id or URL]:thread:' ;;
          esac
        fi
        ;;
      install)
        if [[ -z $leaf ]]; then
          _describe -t install-commands 'install command' install_commands
        fi
        ;;
      group)
        if [[ -z $leaf ]]; then
          _describe -t group-commands 'group command' group_commands
        else
          case $leaf in
            show) _arguments '--group[group id]:group:' ;;
            list|reconcile) _arguments '--group[group id]:group:' '--thread[thread id or URL]:thread:' '--all[all memberships]' '-t[thread id or URL]:thread:' ;;
            *) _arguments '--group[group id]:group:' '--thread[thread id or URL]:thread:' '-t[thread id or URL]:thread:' ;;
          esac
        fi
        ;;
      callback)
        if [[ -z $leaf ]]; then
          _describe -t callback-commands 'callback command' callback_commands
        elif [[ $leaf == register ]]; then
          _arguments '--group[group id]:group:' '--thread[coordinator thread]:thread:' '--pane[exact tmux pane id]:pane:' '-t[coordinator thread]:thread:'
        else
          _arguments '--group[group id]:group:'
        fi
        ;;
      report)
        if [[ -z $leaf ]]; then
          _describe -t report-commands 'report command' report_commands
        else
          case $leaf in
            submit) _arguments '--report-id[stable report id]:id:' '--group[group id]:group:' '--thread[member thread]:thread:' '--status[report status]:status:(ready blocked merged)' '--issue[issue]:issue:' '--reference[reference]:reference:' '--pr[pull request URL]:url:' '--summary[summary]:summary:' '-t[member thread]:thread:' ;;
            pending) _arguments '--group[group id]:group:' '--thread[member thread]:thread:' '--all[all reports]' '-t[member thread]:thread:' ;;
            authorize-finish) _arguments '--report-id[stable report id]:id:' '--thread[coordinator thread]:thread:' '--reference[authorization reference]:reference:' '-t[coordinator thread]:thread:' ;;
            *) _arguments '--report-id[stable report id]:id:' ;;
          esac
        fi
        ;;
      shelve)
        _arguments '--thread[select by thread id or URL]:thread:' '--workspace[select workspace]:workspace:' '--current[current worker]' '--all[all workers]' '-t[select by thread id or URL]:thread:' '-w[select workspace]:workspace:'
        ;;
      unshelve)
        _arguments '--thread[select by thread id or URL]:thread:' '--workspace[select workspace]:workspace:' '--current[current worker]' '--all[all workers]' '-t[select by thread id or URL]:thread:' '-w[select workspace]:workspace:'
        ;;
      spawn)
        _arguments '--workspace[workspace]:workspace:' '--window[window]:window:' '--workdir[working directory]:directory:_directories' '--mode[thread mode]:mode:(low medium high ultra)' '-m[thread mode]:mode:(low medium high ultra)' '--title-prefix[window and thread title prefix]:prefix:' '*--group[durable group id]:group:' '--message[initial message]:message:' '--message-file[read initial message from file]:message file:_files' '--message-stdin[read initial message from stdin]' '--idempotency-key[operation key]:key:' '-w[workspace]:workspace:' '-W[window]:window:' '-d[working directory]:directory:_directories'
        ;;
      teardown)
        _arguments '--thread[select by thread id or URL]:thread:' '--workspace[select workspace]:workspace:' '--current[current worker]' '--all[all workers]' '-t[select by thread id or URL]:thread:' '-w[select workspace]:workspace:'
        ;;
      completion)
        _values 'shell' bash zsh fish
        ;;
    esac
    ;;
esac`)
}

func writeFishCompletion(w io.Writer) {
	fmt.Fprintln(w, "# fish completion for amux; generated by amux completion fish")
	fmt.Fprintln(w, `function __fish_amux_root_command
    set -l words (commandline -opc)
    set -l i 2
    while test $i -le (count $words)
        set -l word $words[$i]
        switch $word
            case --config-dir -c
                set i (math $i + 2)
                continue
            case '--config-dir=*' '-c=*' --json -j --dry-run -n
                set i (math $i + 1)
                continue
            case '*'
                echo $word
                return
        end
    end
end

function __fish_amux_worker_leaf
    set -l words (commandline -opc)
    set -l i 2
    while test $i -le (count $words)
        set -l word $words[$i]
        switch $word
            case --config-dir -c
                set i (math $i + 2)
                continue
            case '--config-dir=*' '-c=*' --json -j --dry-run -n
                set i (math $i + 1)
                continue
            case worker
                set i (math $i + 1)
                if test $i -le (count $words)
                    echo $words[$i]
                end
                return
            case '*'
                return
        end
    end
end

function __fish_amux_install_leaf
    set -l words (commandline -opc)
    set -l index (contains -i -- install $words)
    if test -n "$index"; and test (math $index + 1) -le (count $words)
        echo $words[(math $index + 1)]
    end
end`)
	fmt.Fprintln(w, `
function __fish_amux_group_leaf
    set -l words (commandline -opc)
    set -l index (contains -i -- group $words)
    if test -n "$index"; and test (math $index + 1) -le (count $words)
        echo $words[(math $index + 1)]
    end
end`)
	fmt.Fprintln(w, `
function __fish_amux_report_leaf
    set -l words (commandline -opc)
    set -l index (contains -i -- report $words)
    if test -n "$index"; and test (math $index + 1) -le (count $words)
        echo $words[(math $index + 1)]
    end
end`)
	fmt.Fprintln(w, `
function __fish_amux_callback_leaf
    set -l words (commandline -opc)
    set -l index (contains -i -- callback $words)
    if test -n "$index"; and test (math $index + 1) -le (count $words)
        echo $words[(math $index + 1)]
    end
end`)
	for _, flag := range globalCompletionFlags {
		writeFishFlag(w, "__fish_use_subcommand", flag, flagDescription(flag), flagTakesValue(flag))
	}
	for _, command := range completionCommands {
		fmt.Fprintf(w, "complete -c amux -f -n '__fish_use_subcommand' -a %s -d %s\n", fishQuote(command.Name), fishQuote(command.Description))
	}
	for _, command := range completionCommands {
		condition := fmt.Sprintf("test (__fish_amux_root_command) = %s", command.Name)
		for _, flag := range command.Flags {
			writeFishFlag(w, condition, flag, flagDescription(flag), flagTakesValue(flag))
		}
		if command.Name == "worker" {
			for _, subcommand := range command.Subcommands {
				fmt.Fprintf(w, "complete -c amux -f -n 'test (__fish_amux_root_command) = worker; and test -z (__fish_amux_worker_leaf)' -a %s -d %s\n", fishQuote(subcommand.Name), fishQuote(subcommand.Description))
				condition := fmt.Sprintf("test (__fish_amux_root_command) = worker; and test (__fish_amux_worker_leaf) = %s", subcommand.Name)
				for _, flag := range subcommand.Flags {
					writeFishFlag(w, condition, flag, flagDescription(flag), flagTakesValue(flag))
				}
			}
		}
		if command.Name == "install" {
			fmt.Fprintln(w, "complete -c amux -f -n 'test (__fish_amux_root_command) = install; and test -z (__fish_amux_install_leaf)' -a doctor -d 'Diagnose executable targets, versions, and PATH drift'")
		}
		if command.Name == "group" {
			for _, subcommand := range command.Subcommands {
				fmt.Fprintf(w, "complete -c amux -f -n 'test (__fish_amux_root_command) = group; and test -z (__fish_amux_group_leaf)' -a %s -d %s\n", fishQuote(subcommand.Name), fishQuote(subcommand.Description))
				condition := fmt.Sprintf("test (__fish_amux_root_command) = group; and test (__fish_amux_group_leaf) = %s", subcommand.Name)
				for _, flag := range subcommand.Flags {
					writeFishFlag(w, condition, flag, flagDescription(flag), flagTakesValue(flag))
				}
			}
		}
		if command.Name == "report" {
			for _, subcommand := range command.Subcommands {
				fmt.Fprintf(w, "complete -c amux -f -n 'test (__fish_amux_root_command) = report; and test -z (__fish_amux_report_leaf)' -a %s -d %s\n", fishQuote(subcommand.Name), fishQuote(subcommand.Description))
				condition := fmt.Sprintf("test (__fish_amux_root_command) = report; and test (__fish_amux_report_leaf) = %s", subcommand.Name)
				for _, flag := range subcommand.Flags {
					writeFishFlag(w, condition, flag, flagDescription(flag), flagTakesValue(flag))
				}
			}
		}
		if command.Name == "callback" {
			for _, subcommand := range command.Subcommands {
				fmt.Fprintf(w, "complete -c amux -f -n 'test (__fish_amux_root_command) = callback; and test -z (__fish_amux_callback_leaf)' -a %s -d %s\n", fishQuote(subcommand.Name), fishQuote(subcommand.Description))
				condition := fmt.Sprintf("test (__fish_amux_root_command) = callback; and test (__fish_amux_callback_leaf) = %s", subcommand.Name)
				for _, flag := range subcommand.Flags {
					writeFishFlag(w, condition, flag, flagDescription(flag), flagTakesValue(flag))
				}
			}
		}
		if command.Name == "completion" {
			fmt.Fprintln(w, "complete -c amux -f -n 'test (__fish_amux_root_command) = completion' -a 'bash zsh fish'")
		}
	}
}

func writeFishFlag(w io.Writer, condition, flag, description string, takesValue bool) {
	short, long := fishFlagNames(flag)
	parts := []string{"complete", "-c", "amux", "-n", fishQuote(condition)}
	if takesValue {
		parts = append(parts, "-r")
	} else {
		parts = append(parts, "-f")
	}
	if flag == "--mode" || flag == "-m" {
		parts = append(parts, "-f", "-a", fishQuote(ampDialModeCompletions))
	}
	if short != "" {
		parts = append(parts, "-s", fishQuote(short))
	}
	if long != "" {
		parts = append(parts, "-l", fishQuote(long))
	}
	parts = append(parts, "-d", fishQuote(description))
	fmt.Fprintln(w, strings.Join(parts, " "))
}

func commandNames(commands []completionCommand) []string {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, command.Name)
	}
	return names
}

func workerCompletionFlags() []string {
	seen := make(map[string]bool)
	var flags []string
	for _, command := range completionCommands[0].Subcommands {
		for _, flag := range command.Flags {
			if !seen[flag] {
				seen[flag] = true
				flags = append(flags, flag)
			}
		}
	}
	return flags
}

func runnerCompletionCommands() []completionCommand {
	for _, command := range completionCommands {
		if command.Name == "runner" {
			return command.Subcommands
		}
	}
	return nil
}

func groupCompletionCommands() []completionCommand {
	for _, command := range completionCommands {
		if command.Name == "group" {
			return command.Subcommands
		}
	}
	return nil
}

func reportCompletionCommands() []completionCommand {
	for _, command := range completionCommands {
		if command.Name == "report" {
			return command.Subcommands
		}
	}
	return nil
}

func callbackCompletionCommands() []completionCommand {
	for _, command := range completionCommands {
		if command.Name == "callback" {
			return command.Subcommands
		}
	}
	return nil
}

func flagDescription(flag string) string {
	switch flag {
	case "--config-dir":
		return "Path to config directory"
	case "-c":
		return "Path to config directory"
	case "--json":
		return "Emit one JSON result document"
	case "-j":
		return "Emit one JSON result document"
	case "--dry-run":
		return "Print intended actions without mutating"
	case "-n":
		return "Print intended actions without mutating"
	case "--attach":
		return "Always attach after launch"
	case "--no-attach":
		return "Never attach after launch"
	case "--terminal-launcher":
		return "Terminal launcher command"
	case "--status":
		return "Report status"
	case "--report-id":
		return "Stable report ID"
	case "--pane":
		return "Exact tmux pane ID"
	case "--issue":
		return "Issue identifier"
	case "--reference":
		return "Durable reference"
	case "--pr":
		return "Pull request URL"
	case "--summary":
		return "Bounded report summary"
	case "--active":
		return "Only confirmed active rows"
	case "--shelved":
		return "Only confirmed shelved rows"
	case "--thread", "-t":
		return "Select by thread id or URL"
	case "--group":
		return "Select group ID"
	case "--workspace", "-w":
		return "Select workspace rows"
	case "--window", "-W":
		return "Select worker window"
	case "--workdir", "-d":
		return "Select working directory"
	case "--shelf":
		return "Filter shelf intent"
	case "--session":
		return "Tmux session"
	case "--mode", "-m":
		return "Amp thread mode"
	case "--title-prefix":
		return "Window and thread title prefix"
	case "--message-file":
		return "Read initial message from file"
	case "--message":
		return "Initial worker message"
	case "--message-stdin":
		return "Read initial message from stdin"
	case "--idempotency-key":
		return "Stable spawn operation key"
	case "--include-runners":
		return "Include runner-only workspaces"
	case "--help", "-h":
		return "Print help"
	case "--version":
		return "Print version"
	default:
		return flag
	}
}

func flagTakesValue(flag string) bool {
	switch flag {
	case "--config-dir", "-c", "--thread", "-t", "--group", "--pane", "--workspace", "-w", "--window", "-W", "--workdir", "-d", "--shelf", "--mode", "-m", "--title-prefix", "--message", "--message-file", "--idempotency-key", "--report-id", "--status", "--issue", "--reference", "--pr", "--summary":
		return true
	default:
		return false
	}
}

func fishFlagNames(flag string) (short, long string) {
	if strings.HasPrefix(flag, "--") {
		return "", strings.TrimPrefix(flag, "--")
	}
	if strings.HasPrefix(flag, "-") {
		return strings.TrimPrefix(flag, "-"), ""
	}
	return "", flag
}

func fishQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
