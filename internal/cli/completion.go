package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
)

// completionShells are the shells cmarkfind writes scripts for.
var completionShells = []string{"bash", "powershell"}

// completionFileFlags take a file or folder name, so completion offers files
// after them. Each must be a value flag of some verb, or -db (a test checks).
var completionFileFlags = []string{"db", "index"}

// completionGlobalFlags go before the verb.
var completionGlobalFlags = []string{"-db", "-help", "-license", "-version"}

var nonIdent = regexp.MustCompile(`[^A-Za-z0-9_]`)

func normalizeShell(shell string) string {
	switch s := strings.ToLower(strings.TrimSpace(shell)); s {
	case "pwsh":
		return "powershell"
	default:
		return s
	}
}

// flagWords are a verb's flags as typed, "-name", in name order.
func flagWords(path string) ([]string, error) {
	fl, err := verbFlags(path)
	if err != nil {
		return nil, err
	}
	var w []string
	for _, f := range fl {
		w = append(w, "-"+f.Name)
	}
	sort.Strings(w)
	return w, nil
}

// valueFlagWords are the flags that take a value, in the one-dash and two-dash
// forms, and the subset of them that take a file name.
func valueFlagWords() (all, nonFile []string, err error) {
	seen := map[string]bool{"db": true}
	for _, v := range verbSpecs() {
		for _, path := range v.Paths() {
			fl, err := verbFlags(path)
			if err != nil {
				return nil, nil, err
			}
			for _, f := range fl {
				if !f.Bool {
					seen[f.Name] = true
				}
			}
		}
	}
	var names []string
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	isFile := map[string]bool{}
	for _, f := range completionFileFlags {
		isFile[f] = true
	}
	for _, n := range names {
		all = append(all, "-"+n, "--"+n)
		if !isFile[n] {
			nonFile = append(nonFile, "-"+n, "--"+n)
		}
	}
	return all, nonFile, nil
}

func fileFlagWords() []string {
	var w []string
	for _, n := range completionFileFlags {
		w = append(w, "-"+n, "--"+n)
	}
	return w
}

func helpTopicWords() []string {
	w := []string{"index"}
	for _, t := range Topics {
		w = append(w, t.Name)
	}
	sort.Strings(w)
	return w
}

func psList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = "'" + s + "'"
	}
	return strings.Join(q, ", ")
}

// verbFlagCases emits, for each verb, the arm that lists its flags. bash is
// true for case arms, false for PowerShell switch arms.
func verbFlagCases(bash bool) (string, error) {
	var b strings.Builder
	arm := func(indent, name string, words []string) {
		if bash {
			fmt.Fprintf(&b, "%s%s) COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") ) ;;\n", indent, name, strings.Join(words, " "))
		} else {
			fmt.Fprintf(&b, "%s'%s' { %s }\n", indent, name, "@("+psList(words)+")")
		}
	}
	specs := verbSpecs()
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	for _, v := range specs {
		if len(v.Subverbs) == 0 {
			w, err := flagWords(v.Name)
			if err != nil {
				return "", err
			}
			arm("            ", v.Name, w)
			continue
		}
		// A verb with subverbs: the flags are the subverb's, the last one
		// (the bare verb's default) when none is typed yet.
		last := v.Subverbs[len(v.Subverbs)-1]
		if bash {
			fmt.Fprintf(&b, "            %s) case \"${pos[1]}\" in\n", v.Name)
		} else {
			fmt.Fprintf(&b, "            '%s' { switch ($pos[1]) {\n", v.Name)
		}
		for _, s := range v.Subverbs {
			w, err := flagWords(v.Name + " " + s)
			if err != nil {
				return "", err
			}
			if s == last {
				if bash {
					fmt.Fprintf(&b, "                *) COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") ) ;;\n", strings.Join(w, " "))
				} else {
					fmt.Fprintf(&b, "                default { @(%s) }\n", psList(w))
				}
				continue
			}
			arm("                ", s, w)
		}
		if bash {
			b.WriteString("                esac ;;\n")
		} else {
			b.WriteString("            } }\n")
		}
	}
	return b.String(), nil
}

// verbSubverbCases emits the arms that complete a verb's subverbs.
func verbSubverbCases(bash bool) string {
	var b strings.Builder
	for _, v := range verbSpecs() {
		if len(v.Subverbs) == 0 {
			continue
		}
		if bash {
			fmt.Fprintf(&b, "        %s) [[ $n -eq 1 ]] && COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") ) ;;\n", v.Name, strings.Join(v.Subverbs, " "))
		} else {
			fmt.Fprintf(&b, "            '%s' { if ($n -eq 1) { @(%s) } }\n", v.Name, psList(v.Subverbs))
		}
	}
	return b.String()
}

/** WriteCompletion writes a shell completion script for the named shell. The
 * verbs, subverbs, flags and help topics are read from the registry, so a new
 * verb or flag is completed without being named here.
 *
 * Parameters:
 *   w (io.Writer)    — destination for the script
 *   appName (string) — binary name to register; a .exe suffix is ignored
 *   shell (string)   — "bash" or "powershell" (case-insensitive; "pwsh" is accepted)
 *
 * Returns:
 *   error — a usage error for an unsupported shell, or the write's error
 *
 * Example:
 *   err := cli.WriteCompletion(os.Stdout, "cmarkfind", "bash")
 */
func WriteCompletion(w io.Writer, appName, shell string) error {
	name := strings.TrimSuffix(appName, ".exe")
	var tmpl string
	var bash bool
	switch normalizeShell(shell) {
	case "bash":
		tmpl, bash = bashCompletion, true
	case "powershell":
		tmpl = powershellCompletion
	default:
		return fmt.Errorf("%w: unsupported shell %q; use one of: %s", ErrUsage, shell, strings.Join(completionShells, ", "))
	}
	flagCases, err := verbFlagCases(bash)
	if err != nil {
		return err
	}
	valueAll, valueNonFile, err := valueFlagWords()
	if err != nil {
		return err
	}
	verbs := verbNames()
	sort.Strings(verbs)
	r := strings.NewReplacer(
		"{app_name}", name,
		"{app_func}", nonIdent.ReplaceAllString(name, "_"),
		"{verbs}", strings.Join(verbs, " "),
		"{global_flags}", strings.Join(completionGlobalFlags, " "),
		"{topics}", strings.Join(helpTopicWords(), " "),
		"{shells}", strings.Join(completionShells, " "),
		"{value_flags}", strings.Join(valueAll, "|"),
		"{nonfile_value_flags}", strings.Join(valueNonFile, "|"),
		"{file_flags}", strings.Join(fileFlagWords(), "|"),
		"{flag_cases}", flagCases,
		"{subverb_cases}", verbSubverbCases(bash),
		"{ps_verbs}", psList(verbs),
		"{ps_global_flags}", psList(completionGlobalFlags),
		"{ps_topics}", psList(helpTopicWords()),
		"{ps_shells}", psList(completionShells),
		"{ps_value_flags}", psList(valueAll),
		"{ps_nonfile_value_flags}", psList(valueNonFile),
		"{ps_file_flags}", psList(fileFlagWords()),
	)
	_, err = io.WriteString(w, r.Replace(tmpl))
	return err
}

const bashCompletion = `# bash completion for {app_name}
# Load with:  eval "$({app_name} completion bash)"
# Or install: {app_name} completion bash -install
# (Plain "source" of a process substitution does nothing, silently, in macOS bash 3.2.)
_{app_func}() {
    local cur prev i w n verb
    local -a pos=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    # The words before the cursor that are neither options nor option values.
    for (( i=1; i<COMP_CWORD; i++ )); do
        w="${COMP_WORDS[i]}"
        case "$w" in
            {value_flags}) (( i++ )) ;;
            -*) ;;
            *) pos+=("$w") ;;
        esac
    done
    case "$prev" in
        {file_flags})
            COMPREPLY=( $(compgen -f -- "$cur") )
            return 0 ;;
        {nonfile_value_flags})
            return 0 ;;
    esac
    n=${#pos[@]}
    verb="${pos[0]}"
    if [[ -z "$verb" ]]; then
        if [[ "$cur" == -* ]]; then
            COMPREPLY=( $(compgen -W "{global_flags}" -- "$cur") )
        else
            COMPREPLY=( $(compgen -W "{verbs}" -- "$cur") )
        fi
        return 0
    fi
    if [[ "$cur" == -* ]]; then
        case "$verb" in
{flag_cases}            help) COMPREPLY=( $(compgen -W "-json" -- "$cur") ) ;;
        esac
        return 0
    fi
    case "$verb" in
        help)       [[ $n -eq 1 ]] && COMPREPLY=( $(compgen -W "{topics}" -- "$cur") ) ;;
        completion) [[ $n -eq 1 ]] && COMPREPLY=( $(compgen -W "{shells}" -- "$cur") ) ;;
{subverb_cases}    esac
    return 0
}
complete -o default -F _{app_func} {app_name}
`

const powershellCompletion = `# PowerShell completion for {app_name}
# Load with:  {app_name} completion powershell | Out-String | Invoke-Expression
# Or install: {app_name} completion powershell -install
Register-ArgumentCompleter -Native -CommandName {app_name},{app_name}.exe -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $verbs         = @({ps_verbs})
    $globals       = @({ps_global_flags})
    $topics        = @({ps_topics})
    $shells        = @({ps_shells})
    $valueFlags    = @({ps_value_flags})
    $nonFileValues = @({ps_nonfile_value_flags})
    $fileFlags     = @({ps_file_flags})

    function Get-Files {
        Get-ChildItem -Path "$wordToComplete*" -ErrorAction SilentlyContinue | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'ProviderItem', $_.Name)
        }
    }

    # Words typed after the command name, not counting the word being completed.
    $words = @($commandAst.CommandElements | Select-Object -Skip 1 | ForEach-Object { $_.ToString() })
    if ($wordToComplete -ne '' -and $words.Count -gt 0) {
        $words = @($words | Select-Object -SkipLast 1)
    }
    $prev = if ($words.Count -gt 0) { $words[-1] } else { '' }

    # The words that are neither options nor option values.
    $pos = @()
    for ($i = 0; $i -lt $words.Count; $i++) {
        $w = $words[$i]
        if ($w -in $valueFlags) { $i++; continue }
        if ($w.StartsWith('-')) { continue }
        $pos += $w
    }
    $n = $pos.Count

    if ($prev -in $fileFlags) { return Get-Files }
    if ($prev -in $nonFileValues) { return }
    $candidates = $null
    if ($n -eq 0) {
        $candidates = if ($wordToComplete.StartsWith('-')) { $globals } else { $verbs }
    } elseif ($wordToComplete.StartsWith('-')) {
        $candidates = switch ($pos[0]) {
{flag_cases}            'help' { @('-json') }
            default { $null }
        }
    } else {
        $candidates = switch ($pos[0]) {
            'help'       { if ($n -eq 1) { $topics } }
            'completion' { if ($n -eq 1) { $shells } }
{subverb_cases}            default      { $null }
        }
    }

    if ($null -eq $candidates) { return Get-Files }
    $candidates | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`

/** bashStartupFile returns the file a new bash session reads that completion
 * adds its line to. On Linux that is ~/.bashrc. On macOS Terminal starts a login
 * shell, which reads the first of ~/.bash_profile, ~/.bash_login and ~/.profile
 * that exists, so the line goes there: creating ~/.bash_profile beside an
 * existing ~/.profile would hide the .profile from bash.
 *
 * Parameters:
 *   home (string) — the user's home directory
 *   goos (string) — runtime.GOOS
 *
 * Returns:
 *   string — the startup file's path, which may not exist yet
 *
 * Example:
 *   rc := bashStartupFile(home, "darwin") // ~/.bash_profile
 */
func bashStartupFile(home, goos string) string {
	if goos != "darwin" {
		return filepath.Join(home, ".bashrc")
	}
	for _, name := range []string{".bash_profile", ".bash_login", ".profile"} {
		if _, err := os.Stat(filepath.Join(home, name)); err == nil {
			return filepath.Join(home, name)
		}
	}
	return filepath.Join(home, ".bash_profile")
}

// completionTargets returns where the completion script for a shell lives, which
// startup file refers to it, and the line that file carries.
func completionTargets(name, shell, home, dataHome, goos string) (script, startup, line string) {
	if normalizeShell(shell) == "bash" {
		if dataHome == "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
		script = filepath.Join(dataHome, "bash-completion", "completions", name)
		return script, bashStartupFile(home, goos), fmt.Sprintf("[ -f '%s' ] && . '%s' # %s completion", script, script, name)
	}
	dir := filepath.Join(home, ".config", "powershell")
	if goos == "windows" {
		dir = filepath.Join(home, "Documents", "PowerShell")
	}
	script = filepath.Join(dir, name+"-completion.ps1")
	return script, filepath.Join(dir, "Microsoft.PowerShell_profile.ps1"), fmt.Sprintf(". '%s' # %s completion", script, name)
}

// completionHeader is the first line of a script cmarkfind wrote, which is how
// it knows a file is its own.
func completionHeader(shell, name string) string {
	if normalizeShell(shell) == "bash" {
		return "# bash completion for " + name + "\n"
	}
	return "# PowerShell completion for " + name + "\n"
}

// ioError classes a failed read or write: the OS refusing is exit 77, anything
// else is exit 74.
func ioError(err error) error {
	if err == nil || errors.Is(err, fs.ErrPermission) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrIO, err)
}

// writeFileIn writes data to path, creating its directory first. A directory that
// cannot be made is cant_create (exit 73); a file that cannot be written is io.
func writeFileIn(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrCantCreate, err)
	}
	return ioError(os.WriteFile(path, data, 0o644))
}

// addStartupLine appends line to the startup file unless a line mentioning the
// script is already there. A missing file is created.
func addStartupLine(startup, script, line string) error {
	cur, err := os.ReadFile(startup)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ioError(err)
	}
	if strings.Contains(string(cur), script) {
		return nil
	}
	if len(cur) > 0 && !bytes.HasSuffix(cur, []byte("\n")) {
		cur = append(cur, '\n')
	}
	return writeFileIn(startup, append(cur, line+"\n"...))
}

// removeStartupLines deletes the lines completion added to the startup file for
// the script, reporting whether any were there. Other lines are kept as they are.
func removeStartupLines(startup, script, name string) (bool, error) {
	cur, err := os.ReadFile(startup)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, ioError(err)
	}
	marker := "# " + name + " completion"
	var kept []string
	removed := false
	for _, l := range strings.SplitAfter(string(cur), "\n") {
		if strings.Contains(l, "'"+script+"'") && strings.HasSuffix(strings.TrimRight(l, "\r\n"), marker) {
			removed = true
			continue
		}
		kept = append(kept, l)
	}
	if !removed {
		return false, nil
	}
	return true, ioError(os.WriteFile(startup, []byte(strings.Join(kept, "")), 0o644))
}

func supportedShell(shell string) error {
	if s := normalizeShell(shell); s != "bash" && s != "powershell" {
		return fmt.Errorf("%w: unsupported shell %q; use one of: %s", ErrUsage, shell, strings.Join(completionShells, ", "))
	}
	return nil
}

/** InstallCompletion installs the completion script for a shell so it loads in
 * every new session, and returns the path of the script it wrote.
 *
 * The script goes in the bash-completion user directory
 * ($XDG_DATA_HOME/bash-completion/completions/NAME, or the same under
 * ~/.local/share) for bash and beside the profile for PowerShell. An existing
 * bash file there is replaced only if cmarkfind wrote it. The shell's startup
 * file (bashStartupFile, or the PowerShell profile) gets one line that sources
 * the script, added once: that line is what makes it work where no
 * bash-completion loader reads the directory, as with macOS's /bin/bash 3.2.
 *
 * Parameters:
 *   appName (string)  — binary name to register; a .exe suffix is ignored
 *   shell (string)    — "bash" or "powershell" ("pwsh" is accepted)
 *   home (string)     — the user's home directory
 *   dataHome (string) — $XDG_DATA_HOME, or "" for the default
 *   goos (string)     — runtime.GOOS, which picks the startup file and profile
 *
 * Returns:
 *   (string, error) — the installed script's path; a usage error for an
 *   unsupported shell, a cant_create error for a foreign file in the way, an io
 *   error for a startup file that cannot be written
 *
 * Example:
 *   path, err := cli.InstallCompletion("cmarkfind", "bash", home, "", "linux")
 */
func InstallCompletion(appName, shell, home, dataHome, goos string) (string, error) {
	name := strings.TrimSuffix(appName, ".exe")
	var script bytes.Buffer
	if err := WriteCompletion(&script, appName, shell); err != nil {
		return "", err
	}
	path, startup, line := completionTargets(name, shell, home, dataHome, goos)
	if normalizeShell(shell) == "bash" {
		if old, err := os.ReadFile(path); err == nil {
			if !strings.HasPrefix(string(old), completionHeader(shell, name)) {
				return "", fmt.Errorf("%w: %s exists and was not written by %s; not overwriting it", ErrCantCreate, path, name)
			}
		} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			// ENOTDIR: a file stands where the script's directory must go; that
			// is not a read failure, writeFileIn reports it as cant_create.
			return "", ioError(err)
		}
	}
	if err := writeFileIn(path, script.Bytes()); err != nil {
		return "", err
	}
	return path, addStartupLine(startup, path, line)
}

/** UninstallCompletion undoes InstallCompletion: it removes the script if
 * cmarkfind wrote it and the line added to the startup file. A file cmarkfind
 * did not write is left alone.
 *
 * Parameters:
 *   appName, shell, home, dataHome, goos — as for InstallCompletion
 *
 * Returns:
 *   ([]string, error) — what was removed, one description per item; empty when
 *   nothing was found
 *
 * Example:
 *   removed, err := cli.UninstallCompletion("cmarkfind", "bash", home, "", "linux")
 */
func UninstallCompletion(appName, shell, home, dataHome, goos string) ([]string, error) {
	name := strings.TrimSuffix(appName, ".exe")
	if err := supportedShell(shell); err != nil {
		return nil, err
	}
	path, startup, _ := completionTargets(name, shell, home, dataHome, goos)
	var removed []string
	if old, err := os.ReadFile(path); err == nil {
		if strings.HasPrefix(string(old), completionHeader(shell, name)) {
			if err := os.Remove(path); err != nil {
				return removed, ioError(err)
			}
			removed = append(removed, "script "+path)
		}
	} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
		return nil, ioError(err)
	}
	// The line may be in a startup file other than the one install would pick
	// today (a ~/.bash_profile created since), so look in all of them.
	files := []string{startup}
	if normalizeShell(shell) == "bash" {
		for _, n := range []string{".bashrc", ".bash_profile", ".bash_login", ".profile"} {
			files = append(files, filepath.Join(home, n))
		}
	}
	seen := map[string]bool{}
	for _, f := range files {
		if seen[f] {
			continue
		}
		seen[f] = true
		if ok, err := removeStartupLines(f, path, name); err != nil {
			return removed, err
		} else if ok {
			removed = append(removed, "line in "+f)
		}
	}
	return removed, nil
}

/** CheckCompletion reports, one line per finding, whether completion is set up
 * for a shell and what to do about it. It looks at the files only: it cannot see
 * inside the shell a terminal window is running, so for bash the report ends
 * with the command that does.
 *
 * Parameters:
 *   w (io.Writer)                          — the report
 *   appName, shell, home, dataHome, goos   — as for InstallCompletion
 *
 * Returns:
 *   (bool, error) — true when every check passed; a usage error for an
 *   unsupported shell, or a failed read
 *
 * Example:
 *   ok, err := cli.CheckCompletion(os.Stdout, "cmarkfind", "bash", home, "", "linux")
 */
func CheckCompletion(w io.Writer, appName, shell, home, dataHome, goos string) (bool, error) {
	name := strings.TrimSuffix(appName, ".exe")
	var want bytes.Buffer
	if err := WriteCompletion(&want, appName, shell); err != nil {
		return false, err
	}
	path, startup, _ := completionTargets(name, shell, home, dataHome, goos)
	ok := true
	report := func(good bool, format string, a ...any) {
		mark := "ok     "
		if !good {
			mark, ok = "PROBLEM", false
		}
		fmt.Fprintf(w, "%s %s\n", mark, fmt.Sprintf(format, a...))
	}
	sh := normalizeShell(shell)
	old, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		report(false, "not installed: no script at %s; run: %s completion %s -install", path, name, sh)
	case err != nil:
		return false, ioError(err)
	case !strings.HasPrefix(string(old), completionHeader(shell, name)):
		report(false, "%s exists but %s did not write it", path, name)
	case string(old) != want.String():
		report(false, "script at %s is out of date; run: %s completion %s -install", path, name, sh)
	default:
		report(true, "script is installed and current: %s", path)
	}
	if b, err := os.ReadFile(startup); err == nil && strings.Contains(string(b), "'"+path+"'") {
		report(true, "%s sources it", startup)
	} else {
		report(false, "%s does not source the script; run: %s completion %s -install", startup, name, sh)
	}
	if sh == "bash" {
		fmt.Fprintf(w, "\nA window opened before the install has not read the new line: open a new one, or run: . %s\n", path)
		fmt.Fprintf(w, "To see what the running shell has, type:\n  echo $BASH_VERSION; complete -p %s\n", name)
		fmt.Fprintf(w, "It should print: complete -o default -F _%s %s\n", nonIdent.ReplaceAllString(name, "_"), name)
	}
	return ok, nil
}

func (o *out) completion(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("completion", o)
	install := fset.Bool("install", false, "")
	uninstall := fset.Bool("uninstall", false, "")
	check := fset.Bool("check", false, "")
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	const usage = "completion bash|powershell [-install | -uninstall | -check]"
	// The shell may come before or after the options: parse again past it.
	rest := fset.Args()
	if len(rest) > 1 {
		if err := parse(fset, rest[1:], o, j); err != nil {
			return err
		}
		rest = append(rest[:1:1], fset.Args()...)
	}
	if len(rest) != 1 {
		return fmt.Errorf("%w: %s", ErrUsage, usage)
	}
	n := 0
	for _, b := range []bool{*install, *uninstall, *check} {
		if b {
			n++
		}
	}
	if n > 1 {
		return fmt.Errorf("%w: -install, -uninstall and -check are exclusive: %s", ErrUsage, usage)
	}
	shell := rest[0]
	if err := supportedShell(shell); err != nil {
		return err
	}
	if n == 0 {
		return WriteCompletion(o.stdout, appName, shell)
	}
	sh := normalizeShell(shell)
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("%w: cannot find your home directory: %v", fs.ErrNotExist, err)
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	switch {
	case *uninstall:
		removed, err := UninstallCompletion(appName, shell, home, dataHome, runtime.GOOS)
		if err != nil {
			return err
		}
		if o.json {
			if removed == nil {
				removed = []string{}
			}
			b, _ := json.Marshal(map[string]any{"shell": sh, "removed": removed})
			fmt.Fprintf(o.stdout, "%s\n", b)
		} else {
			for _, r := range removed {
				fmt.Fprintf(o.stdout, "removed %s\n", r)
			}
		}
		if len(removed) == 0 {
			return negative("nothing to uninstall: no %s completion written by %s was found", sh, appName)
		}
		return nil
	case *check:
		var report io.Writer = o.stdout
		if o.json {
			report = io.Discard
		}
		ok, err := CheckCompletion(report, appName, shell, home, dataHome, runtime.GOOS)
		if err != nil {
			return err
		}
		if o.json {
			b, _ := json.Marshal(map[string]any{"shell": sh, "ok": ok})
			fmt.Fprintf(o.stdout, "%s\n", b)
		}
		if !ok {
			return negative("completion for %s is not set up correctly (see above)", sh)
		}
		return nil
	}
	path, err := InstallCompletion(appName, shell, home, dataHome, runtime.GOOS)
	if err != nil {
		return err
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"shell": sh, "path": path})
		fmt.Fprintf(o.stdout, "%s\n", b)
		return nil
	}
	fmt.Fprintf(o.stdout, "installed %s completion: %s\n", sh, path)
	if sh == "bash" {
		fmt.Fprintf(o.stdout, "open a new terminal window, or run: . %s\n", path)
	}
	return nil
}
