package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func script(t *testing.T, shell string) string {
	t.Helper()
	r := do(t, "completion", shell)
	if r.code != 0 {
		t.Fatalf("completion %s: %+v", shell, r)
	}
	return r.out
}

func TestCompletionNeedsOneSupportedShell(t *testing.T) {
	for _, args := range [][]string{
		{"completion"},
		{"completion", "zsh"},
		{"completion", "bash", "powershell"},
		{"completion", "bash", "-install", "-check"},
		{"completion", "bash", "-uninstall", "-install"},
	} {
		if r := do(t, args...); r.code != 2 || r.out != "" {
			t.Errorf("%v: %+v, want exit 2 and nothing on standard output", args, r)
		}
	}
	if r := do(t, "completion", "pwsh"); r.code != 0 || !strings.HasPrefix(r.out, "# PowerShell completion for cmarkfind\n") {
		t.Errorf("pwsh is accepted for powershell: %+v", r)
	}
}

func TestCompletionScriptsCarryTheirHeaderAndNameTheApp(t *testing.T) {
	b := script(t, "bash")
	if !strings.HasPrefix(b, "# bash completion for cmarkfind\n") || !strings.Contains(b, "complete -o default -F _cmarkfind cmarkfind") {
		t.Errorf("bash header or registration missing:\n%.200s", b)
	}
	p := script(t, "powershell")
	if !strings.HasPrefix(p, "# PowerShell completion for cmarkfind\n") || !strings.Contains(p, "Register-ArgumentCompleter -Native -CommandName cmarkfind,cmarkfind.exe") {
		t.Errorf("powershell header or registration missing:\n%.200s", p)
	}
	for _, s := range []string{b, p} {
		if strings.Contains(s, "{app_") || strings.Contains(s, "{verbs}") || strings.Contains(s, "{ps_") || strings.Contains(s, "{bash_") {
			t.Error("a placeholder is left unfilled")
		}
	}
}

// Everything the registry knows is in both scripts: no verb, subverb or flag is
// kept in a second list.
func TestCompletionScriptsCoverTheRegistry(t *testing.T) {
	b, p := script(t, "bash"), script(t, "powershell")
	for _, v := range verbSpecs() {
		for _, s := range []string{b, p} {
			if !strings.Contains(s, v.Name) {
				t.Errorf("verb %s missing", v.Name)
			}
			for _, sub := range v.Subverbs {
				if !strings.Contains(s, sub) {
					t.Errorf("subverb %s missing", sub)
				}
			}
		}
		for _, path := range v.Paths() {
			fl, err := verbFlags(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range fl {
				for _, s := range []string{b, p} {
					if !strings.Contains(s, "-"+f.Name) {
						t.Errorf("%s: flag -%s missing", path, f.Name)
					}
				}
			}
		}
	}
	for _, topic := range append(indexNames(t), "index") {
		if !strings.Contains(b, topic) || !strings.Contains(p, topic) {
			t.Errorf("help topic %s missing", topic)
		}
	}
}

func TestTheFileFlagsAreRealValueFlags(t *testing.T) {
	kinds := map[string]bool{}
	for _, v := range verbSpecs() {
		for _, path := range v.Paths() {
			fl, _ := verbFlags(path)
			for _, f := range fl {
				kinds[f.Name] = f.Bool
			}
		}
	}
	for _, name := range completionFileFlags {
		if isBool, ok := kinds[name]; name != "db" && (!ok || isBool) {
			t.Errorf("-%s is listed as a file flag but no verb has it as a value flag", name)
		}
	}
}

// bashReplies runs the generated function in a real bash with the words typed so
// far (a trailing space means the word being completed is empty).
func bashReplies(t *testing.T, scriptText, typed string) []string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	file := filepath.Join(t.TempDir(), "completion.bash")
	if err := os.WriteFile(file, []byte(scriptText), 0o644); err != nil {
		t.Fatal(err)
	}
	words := strings.Fields("cmarkfind " + typed)
	if strings.HasSuffix(typed, " ") || typed == "" {
		words = append(words, "")
	}
	args := append([]string{"--norc", "--noprofile", "-c",
		`source "$1"; n=$2; shift 2; COMP_WORDS=("$@"); COMP_CWORD=$n; _cmarkfind; printf '%s\n' "${COMPREPLY[@]}"`,
		"bash", file, strconv.Itoa(len(words) - 1)}, words...)
	out, err := exec.Command("bash", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, out)
	}
	var got []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			got = append(got, l)
		}
	}
	sort.Strings(got)
	return got
}

var completionCases = []struct{ typed, want string }{
	{"", "completion exclude fill forget help indexer pin search stats tag unexclude unpin untag verbs"},
	{"fi", "fill"},
	{"-", "-db -help -license -version"},
	{"-db x.db se", "search"},
	{"-db x.db ", "completion exclude fill forget help indexer pin search stats tag unexclude unpin untag verbs"},
	{"fill -s", "-site-url"},
	{"fill -b", "-batch"},
	{"fill -pause 5s -u", "-until-done -user-agent"},
	{"search -tag go -l", "-limit"},
	{"search -", "-index -json -limit -pinned -tag"},
	{"indexer ", "build list validate"},
	{"indexer b", "build"},
	{"indexer build -", "-chunk -json"},
	{"indexer list -", "-json"},
	{"completion ", "bash powershell"},
	{"completion bash -", "-check -install -json -uninstall"},
	{"help v", "verbs"},
	{"help ", "completion corpus exclude exit-status fill forget index indexer pin query search stats tag unexclude unpin untag verbs"},
	{"help fill ", ""},
	{"stats -j", "-json"},
}

func TestBashCompletesForReal(t *testing.T) {
	s := script(t, "bash")
	for _, c := range completionCases {
		got := strings.Join(bashReplies(t, s, c.typed), " ")
		if got != c.want {
			t.Errorf("bash %q\n got %q\nwant %q", c.typed, got, c.want)
		}
	}
}

func TestBashCompletesFileNamesAfterTheFileFlags(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "corpus.db"), nil, 0o644)
	t.Chdir(dir)
	got := bashReplies(t, script(t, "bash"), "-db cor")
	if strings.Join(got, " ") != "corpus.db" {
		t.Errorf("file completion after -db: %v", got)
	}
	got = bashReplies(t, script(t, "bash"), "search -index ")
	if strings.Join(got, " ") != "corpus.db" {
		t.Errorf("file completion after -index: %v", got)
	}
}

func pwshReplies(t *testing.T, scriptText, typed string) []string {
	t.Helper()
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("no pwsh")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "completion.ps1")
	driver := filepath.Join(dir, "drive.ps1")
	os.WriteFile(file, []byte(scriptText), 0o644)
	os.WriteFile(driver, []byte(". '"+file+"'\n$s = '"+strings.ReplaceAll("cmarkfind "+typed, "'", "''")+"'\n"+
		"(TabExpansion2 -inputScript $s -cursorColumn $s.Length).CompletionMatches | ForEach-Object CompletionText\n"), 0o644)
	cmd := exec.Command("pwsh", "-NoProfile", "-NonInteractive", "-File", driver)
	cmd.Dir = t.TempDir() // empty, so file completion has nothing to offer
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pwsh: %v\n%s", err, out)
	}
	var got []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			got = append(got, l)
		}
	}
	sort.Strings(got)
	return got
}

func TestPowerShellCompletesForReal(t *testing.T) {
	s := script(t, "powershell")
	for _, c := range completionCases {
		got := strings.Join(pwshReplies(t, s, c.typed), " ")
		if got != c.want {
			t.Errorf("pwsh %q\n got %q\nwant %q", c.typed, got, c.want)
		}
	}
}
