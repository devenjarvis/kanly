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
	const scaleFP = "5e0d8f5590d5" // lib.Scale: a * k → a / k

	var stdout, stderr bytes.Buffer
	if code := dispatch([]string{"show", "./lib", scaleFP}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("show returned %d; stderr: %s", code, stderr.String())
	}
	for _, want := range []string{"lib/lib.go:7:37 in Scale", "mutant:   func Scale(a, k int) int { return a / k }", "kanly test ./lib " + scaleFP} {
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
		{"own test survives", []string{"./lib", scaleFP, "-run", "TestAdd"}, 0, "the tests passed"},
		{"dependent test kills", []string{"--in=./app", "./lib", scaleFP, "-run", "TestArea"}, 1, "go test failed"},
		{"unknown fingerprint", []string{"./lib", "ffffffffffff"}, 1, "has no mutant ffffffffffff"},
		{"missing mutant", []string{"./lib"}, 2, "usage: kanly test"},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
