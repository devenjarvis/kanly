package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
)

func TestMutatedLine(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		col    int
		m      mutation.Mutation
		want   string
		wantOK bool
	}{
		{"operator", "\treturn a * k", 10, mutation.Mutation{Original: "*", Mutant: "/"}, "\treturn a / k", true},
		{"at line start", "x++", 0, mutation.Mutation{Original: "x", Mutant: "y"}, "y++", true},
		{"at line end", "a <", 2, mutation.Mutation{Original: "<", Mutant: "<="}, "a <=", true},
		{"deletion", "if !ok {", 3, mutation.Mutation{Original: "!", Mutant: ""}, "if ok {", true},
		{"symbolic original", "\tadd(line, set)", 1, mutation.Mutation{Original: "add(...)", Mutant: ""}, "", false},
		{"empty original", "return x", 7, mutation.Mutation{Original: "", Mutant: "0"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mutatedLine(tt.line, tt.col, tt.m)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("mutatedLine(%q, %d) = %q, %v; want %q, %v", tt.line, tt.col, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestDescribeMutant(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f.go")
	if err := os.WriteFile(file, []byte("package f\n\n\tfunc F(a int) int { return a - 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := mutation.Mutation{ID: 3, File: file, Line: 3, Column: 31, Function: "F", OperatorName: "int_arith", Original: "-", Mutant: "+", Fingerprint: "45a983f95ba4"}
	var buf bytes.Buffer
	describeMutant(&buf, m)
	want := file + ":3:31 in F\n" +
		"  #3 fp 45a983f95ba4 · int_arith `-` → `+`\n" +
		"  original: \tfunc F(a int) int { return a - 1 }\n" +
		"            \t                             ^\n" +
		"  mutant:   \tfunc F(a int) int { return a + 1 }\n"
	if buf.String() != want {
		t.Errorf("describeMutant =\n%s\nwant\n%s", buf.String(), want)
	}

	// A position past the end of the file prints the header only.
	buf.Reset()
	m.Line = 99
	describeMutant(&buf, m)
	if strings.Contains(buf.String(), "original:") {
		t.Errorf("describeMutant printed a source line for a missing line:\n%s", buf.String())
	}
}

func TestShowAndTestSubcommands(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	chdir(t, relDir(t, "../../internal/runner/testdata/crosspkg"))
	const (
		scaleFP  = "5e0d8f5590d5" // lib.Scale: a * k → a / k
		unusedFP = "45a983f95ba4" // lib.Unused: a - 1 → a + 1
	)

	for _, tt := range []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"two mutants", []string{"--tags=integration", "./lib", scaleFP, unusedFP}, 0,
			"lib/lib.go:7:37 in Scale\n", ""},
		{"missing mutant", []string{"./lib"}, 2, "", "usage: kanly show"},
		{"unknown flag", []string{"--nope", "./lib", scaleFP}, 2, "", "flag provided but not defined: -nope"},
		{"no such directory", []string{"./nope", scaleFP}, 1, "", "kanly show: ./nope: package errors"},
		{"several packages", []string{"./...", scaleFP}, 1, "", "must match exactly one package"},
		{"not a mutant reference", []string{"./lib", "zz"}, 1, "", `"zz" is not a mutant ID or fingerprint`},
		{"empty mutant reference", []string{"./lib", ""}, 1, "", `"" is not a mutant ID or fingerprint`},
		{"unknown fingerprint", []string{"./lib", "ffffffffffff"}, 1, "", "has no mutant ffffffffffff"},
	} {
		t.Run("show "+tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := dispatch(append([]string{"show"}, tt.args...), nil, &stdout, &stderr); code != tt.wantCode {
				t.Errorf("exit %d, want %d; stderr: %s", code, tt.wantCode, stderr.String())
			}
			if !strings.HasPrefix(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout does not start with %q:\n%s", tt.wantStdout, stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr lacks %q: %s", tt.wantStderr, stderr.String())
			}
		})
	}

	var stdout, stderr bytes.Buffer
	if code := dispatch([]string{"show", "./lib", scaleFP, unusedFP}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("show returned %d; stderr: %s", code, stderr.String())
	}
	for _, want := range []string{
		"mutant:   func Scale(a, k int) int { return a / k }\n\nlib/lib.go:10:35 in Unused\n",
		"kanly test ./lib " + scaleFP,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("show output lacks %q:\n%s", want, stdout.String())
		}
	}

	for _, tt := range []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{"own test survives", []string{"./lib", scaleFP, "-run", "TestAdd"}, 0, "with mutant #2 (fp " + scaleFP + ") applied"},
		{"dependent test kills", []string{"--in=./app", "./lib", scaleFP, "-run", "TestArea"}, 1, "go test failed"},
		{"unknown fingerprint", []string{"./lib", "ffffffffffff"}, 1, "kanly test: crosspkg/lib has no mutant ffffffffffff"},
		{"no such directory", []string{"./nope", scaleFP}, 1, "kanly test: ./nope: package errors"},
		{"missing mutant", []string{"./lib"}, 2, "usage: kanly test"},
		{"unknown flag", []string{"--nope", "./lib", scaleFP}, 2, "flag provided but not defined: -nope"},
	} {
		t.Run("test "+tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := dispatch(append([]string{"test"}, tt.args...), nil, &stdout, &stderr); code != tt.wantCode {
				t.Errorf("exit %d, want %d; stdout: %s; stderr: %s", code, tt.wantCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr lacks %q: %s", tt.wantStderr, stderr.String())
			}
		})
	}
}

// TestTestSubcommandPassesTags needs --tags at load time, to find the
// mutant, and in go test, to build the tagged file and test.
func TestTestSubcommandPassesTags(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := relDir(t, "../../internal/runner/testdata/tagsample")
	_, rew, err := loadMutants(dir, "integration")
	if err != nil {
		t.Fatal(err)
	}
	var subFP string
	for _, m := range rew.Mutations {
		if m.Function == "Sub" && m.OperatorName == "int_arith" {
			subFP = m.Fingerprint
		}
	}
	if subFP == "" {
		t.Fatal("no int_arith mutant in Sub")
	}
	var stdout, stderr bytes.Buffer
	if code := dispatch([]string{"test", "--tags=integration", dir, subFP, "-run", "TestSub"}, nil, &stdout, &stderr); code != 1 {
		t.Errorf("exit %d, want 1 (TestSub kills the mutant); stdout: %s; stderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "--- FAIL: TestSub") {
		t.Errorf("TestSub did not fail:\n%s", stdout.String())
	}
}
