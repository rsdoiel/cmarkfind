package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// install runs the verb with HOME and XDG_DATA_HOME in a temporary directory.
func withHome(t *testing.T) (home, data string) {
	t.Helper()
	home, data = t.TempDir(), filepath.Join(t.TempDir(), "share")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_DATA_HOME", data)
	return home, data
}

func TestInstallBashWritesTheScriptAndOneLineInBashrcOnce(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("this test names the Linux startup file")
	}
	home, data := withHome(t)
	rc := filepath.Join(home, ".bashrc")
	os.WriteFile(rc, []byte("export A=1"), 0o644) // no trailing newline
	for i := 0; i < 2; i++ {
		r := do(t, "completion", "bash", "-install")
		if r.code != 0 || !strings.Contains(r.out, "installed bash completion") {
			t.Fatalf("install %d: %+v", i, r)
		}
	}
	script := filepath.Join(data, "bash-completion", "completions", "cmarkfind")
	b, err := os.ReadFile(script)
	if err != nil || !strings.HasPrefix(string(b), "# bash completion for cmarkfind\n") {
		t.Fatalf("script: %v %.60s", err, b)
	}
	got, _ := os.ReadFile(rc)
	want := "export A=1\n[ -f '" + script + "' ] && . '" + script + "' # cmarkfind completion\n"
	if string(got) != want {
		t.Errorf(".bashrc\n got %q\nwant %q", got, want)
	}
}

func TestInstallBashNeverOverwritesAFileItDidNotWrite(t *testing.T) {
	_, data := withHome(t)
	script := filepath.Join(data, "bash-completion", "completions", "cmarkfind")
	os.MkdirAll(filepath.Dir(script), 0o755)
	os.WriteFile(script, []byte("# somebody else's\n"), 0o644)
	r := do(t, "completion", "bash", "-install")
	if r.code != 73 || !strings.Contains(r.errOut, "not overwriting") {
		t.Errorf("%+v", r)
	}
	if b, _ := os.ReadFile(script); string(b) != "# somebody else's\n" {
		t.Errorf("the file was changed: %q", b)
	}
}

func TestInstallReplacesItsOwnOutOfDateScript(t *testing.T) {
	_, data := withHome(t)
	script := filepath.Join(data, "bash-completion", "completions", "cmarkfind")
	os.MkdirAll(filepath.Dir(script), 0o755)
	os.WriteFile(script, []byte("# bash completion for cmarkfind\n# an old one\n"), 0o644)
	if r := do(t, "completion", "bash", "-install"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if b, _ := os.ReadFile(script); strings.Contains(string(b), "an old one") {
		t.Error("the old script was kept")
	}
}

func TestInstallPowerShellWritesBesideTheProfile(t *testing.T) {
	home, _ := withHome(t)
	r := do(t, "completion", "powershell", "-install")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	dir := filepath.Join(home, ".config", "powershell")
	if runtime.GOOS == "windows" {
		dir = filepath.Join(home, "Documents", "PowerShell")
	}
	script := filepath.Join(dir, "cmarkfind-completion.ps1")
	if b, err := os.ReadFile(script); err != nil || !strings.HasPrefix(string(b), "# PowerShell completion for cmarkfind\n") {
		t.Fatalf("script: %v", err)
	}
	prof, _ := os.ReadFile(filepath.Join(dir, "Microsoft.PowerShell_profile.ps1"))
	if strings.Count(string(prof), "cmarkfind completion") != 1 || !strings.Contains(string(prof), ". '"+script+"'") {
		t.Errorf("profile %q", prof)
	}
}

func TestUninstallRemovesWhatInstallWroteAndKeepsTheRest(t *testing.T) {
	home, data := withHome(t)
	rc := filepath.Join(home, ".bashrc")
	os.WriteFile(rc, []byte("export A=1\n"), 0o644)
	do(t, "completion", "bash", "-install")
	r := do(t, "completion", "bash", "-uninstall")
	if r.code != 0 || !strings.Contains(r.out, "removed script") || !strings.Contains(r.out, "removed line in") {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(data, "bash-completion", "completions", "cmarkfind")); err == nil {
		t.Error("the script is still there")
	}
	if b, _ := os.ReadFile(rc); string(b) != "export A=1\n" {
		t.Errorf(".bashrc is %q", b)
	}
	r = do(t, "completion", "bash", "-uninstall")
	if r.code != 1 || !strings.Contains(r.errOut, "nothing to uninstall") {
		t.Errorf("a second uninstall: %+v", r)
	}
}

func TestUninstallLeavesAForeignFileAlone(t *testing.T) {
	_, data := withHome(t)
	script := filepath.Join(data, "bash-completion", "completions", "cmarkfind")
	os.MkdirAll(filepath.Dir(script), 0o755)
	os.WriteFile(script, []byte("# somebody else's\n"), 0o644)
	if r := do(t, "completion", "bash", "-uninstall"); r.code != 1 {
		t.Errorf("%+v", r)
	}
	if _, err := os.Stat(script); err != nil {
		t.Error("a foreign file was removed")
	}
}

func TestCheckReportsWhatIsMissingAndWhatIsFine(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("startup file differs")
	}
	withHome(t)
	r := do(t, "completion", "bash", "-check")
	if r.code != 1 || !strings.Contains(r.out, "PROBLEM") || !strings.Contains(r.out, "not installed") || !strings.Contains(r.out, "cmarkfind completion bash -install") {
		t.Fatalf("before install: %+v", r)
	}
	do(t, "completion", "bash", "-install")
	r = do(t, "completion", "bash", "-check")
	if r.code != 0 || strings.Contains(r.out, "PROBLEM") || !strings.Contains(r.out, "installed and current") || !strings.Contains(r.out, "complete -p cmarkfind") {
		t.Fatalf("after install: %+v", r)
	}
}

func TestCheckNoticesAStaleScriptAndAMissingLine(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("startup file differs")
	}
	home, data := withHome(t)
	do(t, "completion", "bash", "-install")
	script := filepath.Join(data, "bash-completion", "completions", "cmarkfind")
	os.WriteFile(script, []byte("# bash completion for cmarkfind\n# stale\n"), 0o644)
	os.WriteFile(filepath.Join(home, ".bashrc"), nil, 0o644)
	r := do(t, "completion", "bash", "-check")
	if r.code != 1 || !strings.Contains(r.out, "out of date") || !strings.Contains(r.out, "does not source") {
		t.Errorf("%+v", r)
	}
}

func TestCompletionJSONResults(t *testing.T) {
	withHome(t)
	j := parseJSON(t, do(t, "completion", "bash", "-install", "-json").out)
	if j["shell"] != "bash" || !strings.HasSuffix(j["path"].(string), "cmarkfind") {
		t.Errorf("install json %v", j)
	}
	r := do(t, "completion", "bash", "-check", "-json")
	if j := parseJSON(t, r.out); j["ok"] != true || r.code != 0 {
		t.Errorf("check json %v code %d", j, r.code)
	}
	r = do(t, "completion", "bash", "-uninstall", "-json")
	j = parseJSON(t, r.out)
	if rem, _ := j["removed"].([]any); len(rem) != 2 {
		t.Errorf("uninstall json %v", j)
	}
	r = do(t, "completion", "bash", "-uninstall", "-json")
	if j := parseJSON(t, r.out); r.code != 1 || j["removed"] == nil {
		t.Errorf("empty uninstall: code %d, %v", r.code, j)
	}
}

// Exit codes (workspace DR-0003): 66 no home, 73 a directory that cannot be made,
// 74 a startup file that cannot be written after the script was.
func TestCompletionNoHomeDirectoryIsNoInput(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("os.UserHomeDir reads other variables here")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	if r := do(t, "completion", "bash", "-install"); r.code != 66 {
		t.Errorf("%+v", r)
	}
}

func TestCompletionScriptDirectoryThatCannotBeMadeIsCantCreate(t *testing.T) {
	data := t.TempDir()
	os.WriteFile(filepath.Join(data, "bash-completion"), []byte("a file where a directory must go"), 0o644)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", data)
	if r := do(t, "completion", "bash", "-install"); r.code != 73 {
		t.Errorf("%+v", r)
	}
}

func TestCompletionStartupFileThatCannotBeWrittenIsIO(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("startup file differs")
	}
	home, _ := withHome(t)
	_, startup, _ := completionTargets("cmarkfind", "bash", home, "", runtime.GOOS)
	if err := os.MkdirAll(startup, 0o755); err != nil { // a directory where the file must be
		t.Fatal(err)
	}
	if r := do(t, "completion", "bash", "-install"); r.code != 74 {
		t.Errorf("%+v", r)
	}
}

// Every code completion can exit with is on the exit-status page, the main page
// and the completion page.
func TestCompletionExitCodesAreDocumented(t *testing.T) {
	pages := map[string]string{
		"cmarkfind(1)": HelpText,
		"exit-status":  do(t, "help", "exit-status").out,
		"completion":   do(t, "help", "completion").out,
	}
	for name, text := range pages {
		for _, code := range []string{"\n74\n"} {
			if name != "completion" && !strings.Contains(text, code) {
				t.Errorf("%s does not document exit 74", name)
			}
		}
	}
	c := pages["completion"]
	for _, want := range []string{"# EXIT STATUS", "\n1\n", "\n2\n", "\n66\n", "\n73\n", "\n74\n", "-uninstall", "-check", "bash-completion", "XDG_DATA_HOME"} {
		if !strings.Contains(c, want) {
			t.Errorf("the completion page lacks %q", want)
		}
	}
}
