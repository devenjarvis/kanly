package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuideNamesCommandAndBase(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := dispatch([]string{"guide", "--command=go run ./cmd/kanly", "--diff-base=origin/dev"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code: want 0, got %d; stderr: %s", code, stderr.String())
	}
	for _, want := range []string{
		"go run ./cmd/kanly --diff --diff-base=origin/dev --format=llm",
		"go run ./cmd/kanly --format=llm ./path/to/pkg:FuncName",
		"`<fp> <reason>`",
		"Never accept a mutant to get past a score threshold.",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("guide does not contain %q:\n%s", want, stdout.String())
		}
	}
}

func TestGuideDefaults(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := dispatch([]string{"guide"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code: want 0, got %d; stderr: %s", code, stderr.String())
	}
	if want := "kanly --diff --diff-base=origin/main --format=llm"; !strings.Contains(stdout.String(), want) {
		t.Errorf("guide does not contain %q:\n%s", want, stdout.String())
	}
}

func TestGuideRejectsArguments(t *testing.T) {
	for _, tc := range []struct {
		args      []string
		stderrHas string
	}{
		{args: []string{"guide", "extra"}, stderrHas: "usage: kanly guide"},
		{args: []string{"guide", "--nope"}, stderrHas: "-nope"},
	} {
		var stdout, stderr bytes.Buffer
		if code := dispatch(tc.args, nil, &stdout, &stderr); code != 2 {
			t.Errorf("%v: exit code: want 2, got %d", tc.args, code)
		}
		if stdout.Len() != 0 {
			t.Errorf("%v: stdout: want empty, got %q", tc.args, stdout.String())
		}
		if !strings.Contains(stderr.String(), tc.stderrHas) {
			t.Errorf("%v: stderr does not mention %q: %q", tc.args, tc.stderrHas, stderr.String())
		}
	}
}

func TestDispatchWithoutArgumentsShowsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := dispatch(nil, nil, &stdout, &stderr); code != 2 {
		t.Errorf("exit code: want 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "usage: kanly") {
		t.Errorf("stderr does not show the usage: %q", stderr.String())
	}
}

// readStopHooks returns every Stop hook command in a settings file and the
// decoded settings.
func readStopHooks(t *testing.T, path string) ([]map[string]any, map[string]any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings.json is not JSON: %v\n%s", err, data)
	}
	var hooks []map[string]any
	for _, g := range settings["hooks"].(map[string]any)["Stop"].([]any) {
		for _, h := range g.(map[string]any)["hooks"].([]any) {
			hooks = append(hooks, h.(map[string]any))
		}
	}
	return hooks, settings
}

func TestInitWritesSkillAndMergesStopHook(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"permissions": {"allow": ["Bash(go test:*)"]}, "hooks": {"Stop": [{"hooks": [{"type": "command", "command": "./lint.sh"}]}]}}`
	if err := os.WriteFile(settingsPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	// Running init twice, with a new score the second time, must update the
	// hook rather than add a second one.
	var stdout, stderr bytes.Buffer
	for _, run := range []struct{ score, base string }{{"75", "origin/main"}, {"85", "origin/dev"}} {
		stdout.Reset()
		code := dispatch([]string{"init", "--agent=claude", "--dir=" + dir, "--command=go run ./cmd/kanly", "--min-score=" + run.score, "--diff-base=" + run.base}, nil, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit code: want 0, got %d; stderr: %s", code, stderr.String())
		}
	}
	hookCmd := `cd "$CLAUDE_PROJECT_DIR" && go run ./cmd/kanly hook stop --diff-base=origin/dev --min-score=85`
	for _, want := range []string{"wrote " + filepath.Join(dir, ".claude", "skills", "kanly", "SKILL.md"), settingsPath, hookCmd} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout does not mention %q: %q", want, stdout.String())
		}
	}

	skill, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "kanly", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(skill), skillText("go run ./cmd/kanly", "origin/dev"); got != want {
		t.Errorf("SKILL.md:\n%s\nwant:\n%s", got, want)
	}
	if !strings.HasPrefix(string(skill), "---\nname: kanly\ndescription: ") {
		t.Errorf("SKILL.md does not open with name and description frontmatter:\n%s", skill)
	}
	if !strings.Contains(string(skill), "\n\n# Mutation-testing Go changes with Kanly\n") {
		t.Errorf("SKILL.md: the guide's heading is not on a line of its own:\n%s", skill)
	}

	hooks, settings := readStopHooks(t, settingsPath)
	if _, ok := settings["permissions"]; !ok {
		t.Errorf("init dropped the existing permissions: %v", settings)
	}
	want := []map[string]any{
		{"type": "command", "command": "./lint.sh"},
		{"type": "command", "command": hookCmd, "timeout": float64(hookTimeoutSeconds)},
	}
	if len(hooks) != len(want) {
		t.Fatalf("Stop hooks: want %v, got %v", want, hooks)
	}
	for i := range want {
		for k, v := range want[i] {
			if hooks[i][k] != v {
				t.Errorf("Stop hook %d %s: want %v, got %v", i, k, v, hooks[i][k])
			}
		}
		if len(hooks[i]) != len(want[i]) {
			t.Errorf("Stop hook %d: want %v, got %v", i, want[i], hooks[i])
		}
	}
}

func TestInitDefaultsInEmptyProject(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := dispatch([]string{"init", "--agent=claude", "--dir=" + dir}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code: want 0, got %d; stderr: %s", code, stderr.String())
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	if raw, _ := os.ReadFile(settingsPath); !bytes.Contains(raw, []byte("&& kanly hook stop")) {
		t.Errorf("settings.json escapes the hook command:\n%s", raw)
	}
	hooks, settings := readStopHooks(t, settingsPath)
	if len(settings) != 1 {
		t.Errorf("settings: want only \"hooks\", got %v", settings)
	}
	want := `cd "$CLAUDE_PROJECT_DIR" && kanly hook stop --diff-base=origin/main --min-score=80`
	if len(hooks) != 1 || hooks[0]["command"] != want {
		t.Errorf("Stop hooks: want one running %q, got %v", want, hooks)
	}
}

func TestInitKeepsEditedSkill(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, ".claude", "skills", "kanly", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const edited = "---\nname: kanly\n---\nour own rules\n"
	if err := os.WriteFile(skillPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := dispatch([]string{"init", "--agent=claude", "--dir=" + dir, "--no-hook"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code: want 0, got %d; stderr: %s", code, stderr.String())
	}
	got, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != edited {
		t.Errorf("init overwrote an edited skill:\n%s", got)
	}
	// Keeping the file must say how to get generated updates back.
	for _, want := range []string{"kept ", "Generated by"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout does not mention %q: %q", want, stdout.String())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Errorf("--no-hook wrote settings.json (stat err %v)", err)
	}
}

func TestInitRejects(t *testing.T) {
	write := func(rel, content string) func(t *testing.T, dir string) {
		return func(t *testing.T, dir string) {
			path := filepath.Join(dir, rel)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mkdir := func(rel string) func(t *testing.T, dir string) {
		return func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, rel), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		name      string
		args      []string
		setup     func(t *testing.T, dir string)
		want      int
		stderrHas string
	}{
		{name: "no agent", args: []string{"init"}, want: 2, stderrHas: "--agent:"},
		{name: "unknown agent", args: []string{"init", "--agent=cursor"}, want: 2, stderrHas: "kanly guide"},
		{name: "extra argument", args: []string{"init", "--agent=claude", "now"}, want: 2, stderrHas: "usage: kanly init"},
		{name: "bad flag", args: []string{"init", "--nope"}, want: 2, stderrHas: "-nope"},
		{name: "score above 100", args: []string{"init", "--agent=claude", "--min-score=101"}, want: 2, stderrHas: "--min-score"},
		{name: "negative score", args: []string{"init", "--agent=claude", "--min-score=-1"}, want: 2, stderrHas: "--min-score"},
		{name: "malformed settings", args: []string{"init", "--agent=claude"}, setup: write(".claude/settings.json", "{"), want: 1, stderrHas: "settings.json"},
		{name: "hooks not an object", args: []string{"init", "--agent=claude"}, setup: write(".claude/settings.json", `{"hooks": []}`), want: 1, stderrHas: `"hooks"`},
		{name: "Stop not an array", args: []string{"init", "--agent=claude"}, setup: write(".claude/settings.json", `{"hooks": {"Stop": {}}}`), want: 1, stderrHas: `"hooks.Stop"`},
		{name: "settings is a directory", args: []string{"init", "--agent=claude"}, setup: mkdir(".claude/settings.json"), want: 1, stderrHas: "settings.json"},
		{name: "skill is a directory", args: []string{"init", "--agent=claude"}, setup: mkdir(".claude/skills/kanly/SKILL.md"), want: 1, stderrHas: "SKILL.md"},
		{name: "skills dir is a file", args: []string{"init", "--agent=claude"}, setup: write(".claude/skills", ""), want: 1, stderrHas: "skills"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			var stdout, stderr bytes.Buffer
			// --dir goes first: flags after a positional argument are not parsed.
			args := append([]string{tc.args[0], "--dir=" + dir}, tc.args[1:]...)
			if code := dispatch(args, nil, &stdout, &stderr); code != tc.want {
				t.Errorf("exit code: want %d, got %d; stderr: %s", tc.want, code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.stderrHas) {
				t.Errorf("stderr does not mention %q: %q", tc.stderrHas, stderr.String())
			}
		})
	}
}

func TestAddStopHookLeavesInvalidSettingsAlone(t *testing.T) {
	for _, settings := range []string{"{", `{"hooks": []}`, `{"hooks": {"Stop": {}}}`} {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, []byte(settings), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := addStopHook(path, "kanly hook stop"); err == nil {
			t.Errorf("%s: want an error", settings)
		}
		if got, _ := os.ReadFile(path); string(got) != settings {
			t.Errorf("%s: settings changed to %s", settings, got)
		}
	}
}

func TestAddStopHookUnwritableDir(t *testing.T) {
	parent := filepath.Join(t.TempDir(), ".claude")
	if err := os.WriteFile(parent, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := addStopHook(filepath.Join(parent, "settings.json"), "kanly hook stop"); err == nil {
		t.Error("want an error when the settings directory cannot be created")
	}
}

func TestTruncateReason(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		limit    int
		want     string
	}{
		{name: "under", in: "abc", limit: 4, want: "abc"},
		{name: "at limit", in: "abcd", limit: 4, want: "abcd"},
		{name: "over", in: "abcde", limit: 4, want: "abcd[cut]"},
		{name: "inside a rune", in: "ab→", limit: 3, want: "ab[cut]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncateReason(tc.in, tc.limit, "[cut]"); got != tc.want {
				t.Errorf("truncateReason(%q, %d) = %q, want %q", tc.in, tc.limit, got, tc.want)
			}
		})
	}
}

func TestHookStopRejects(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		stdin     string
		want      int
		stderrHas string
	}{
		{name: "no event", args: []string{"hook"}, want: 2, stderrHas: "usage: kanly hook stop"},
		{name: "unknown event", args: []string{"hook", "start"}, want: 2, stderrHas: "usage: kanly hook stop"},
		{name: "bad flag", args: []string{"hook", "stop", "--nope"}, want: 2, stderrHas: "-nope"},
		{name: "bad flag shows usage", args: []string{"hook", "stop", "--nope"}, want: 2, stderrHas: "usage: kanly hook stop"},
		{name: "bad input", args: []string{"hook", "stop"}, stdin: "{", want: 1, stderrHas: "hook input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := dispatch(tc.args, strings.NewReader(tc.stdin), &stdout, &stderr); code != tc.want {
				t.Errorf("exit code: want %d, got %d; stderr: %s", tc.want, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout: want empty, got %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.stderrHas) {
				t.Errorf("stderr does not mention %q: %q", tc.stderrHas, stderr.String())
			}
		})
	}
}

// When Kanly cannot run (here: not a git repository), writing tests would not
// help, so the hook reports it without blocking. Empty input is not an error.
func TestHookStopDoesNotBlockWhenKanlyFails(t *testing.T) {
	chdir(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := dispatch([]string{"hook", "stop"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Errorf("exit code: want 1, got %d; stderr: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout: want no decision, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "not blocking") {
		t.Errorf("stderr does not say the stop was not blocked: %q", stderr.String())
	}
}

// chdir changes the working directory for the rest of the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Error(err)
		}
	})
}

// TestHookStop runs the hook on a repository whose change is tested weakly,
// then well: the first blocks the stop with the brief, the second does not.
func TestHookStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@e.x",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@e.x",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "commit.gpgsign", "false")
	write("go.mod", "module example.com/hook\n\ngo 1.23\n")
	write("positive.go", "package hook\n")
	git("add", ".")
	git("commit", "-q", "-m", "base")

	// The session's change: a new function, tested without an assertion.
	write("positive.go", "package hook\n\nfunc Positive(n int) bool { return n > 0 }\n")
	write("positive_test.go", "package hook\n\nimport \"testing\"\n\nfunc TestPositive(t *testing.T) { Positive(1) }\n")
	chdir(t, repo)

	hook := func(stdin string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := dispatch([]string{"hook", "stop", "--diff-base=main", "--min-score=80"}, strings.NewReader(stdin), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}

	code, out, errOut := hook(`{"hook_event_name": "Stop", "stop_hook_active": false}`)
	if code != 0 {
		t.Fatalf("weak test: exit code: want 0, got %d; stderr: %s", code, errOut)
	}
	var decision struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &decision); err != nil {
		t.Fatalf("weak test: stdout is not a hook decision: %v\n%s", err, out)
	}
	if decision.Decision != "block" {
		t.Errorf("weak test: decision: want block, got %q", decision.Decision)
	}
	for _, want := range []string{"changed since main scores below 80%", "## Live mutants", "kanly --mutant="} {
		if !strings.Contains(decision.Reason, want) {
			t.Errorf("weak test: reason does not contain %q:\n%s", want, decision.Reason)
		}
	}

	// Already continuing because of a stop hook: let the agent stop.
	if code, out, errOut := hook(`{"stop_hook_active": true}`); code != 0 || out != "" {
		t.Errorf("stop_hook_active: want exit 0 and no output, got %d %q; stderr: %s", code, out, errOut)
	}

	write("positive_test.go", `package hook

import "testing"

func TestPositive(t *testing.T) {
	for n, want := range map[int]bool{-1: false, 0: false, 1: true} {
		if got := Positive(n); got != want {
			t.Errorf("Positive(%d) = %v, want %v", n, got, want)
		}
	}
}
`)
	if code, out, errOut := hook(`{"stop_hook_active": false}`); code != 0 || out != "" {
		t.Errorf("strong test: want exit 0 and no output, got %d %q; stderr: %s", code, out, errOut)
	}
}
