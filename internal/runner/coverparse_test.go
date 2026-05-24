package runner

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseCoverProfile(t *testing.T) {
	const profile = `mode: set
multipkg/foo/foo.go:3.30,3.43 1 1
multipkg/foo/foo.go:5.30,5.43 1 0
multipkg/foo/bar.go:10.1,12.20 2 3
`
	got, err := parseCoverProfile([]byte(profile))
	if err != nil {
		t.Fatalf("parseCoverProfile: %v", err)
	}

	want := []coverBlock{
		{File: "multipkg/foo/foo.go", StartLine: 3, EndLine: 3, Count: 1},
		{File: "multipkg/foo/bar.go", StartLine: 10, EndLine: 12, Count: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsed blocks mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestParseCoverProfileMissingHeader(t *testing.T) {
	_, err := parseCoverProfile([]byte("foo.go:1.1,1.2 1 1\n"))
	if err == nil {
		t.Fatal("expected error for missing header")
	}
}

func TestParseCoverProfileEmptyBody(t *testing.T) {
	got, err := parseCoverProfile([]byte("mode: set\n"))
	if err != nil {
		t.Fatalf("parseCoverProfile: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0 blocks, got %d", len(got))
	}
}

func TestParseCoverProfileErrors(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantErrContains string
	}{
		{
			name:            "missing colon separator",
			input:           "mode: set\nno_colon_here\n",
			wantErrContains: "missing ':'",
		},
		{
			name:            "wrong field count",
			input:           "mode: set\nfoo.go:1.1,2.2 1\n",
			wantErrContains: "want 3",
		},
		{
			name:            "count not integer",
			input:           "mode: set\nfoo.go:1.1,2.2 1 abc\n",
			wantErrContains: "count not int",
		},
		{
			name:            "range missing comma",
			input:           "mode: set\nfoo.go:1.12.2 1 1\n",
			wantErrContains: "missing ','",
		},
		{
			name:            "start missing dot",
			input:           "mode: set\nfoo.go:1,2.2 1 1\n",
			wantErrContains: "start missing '.'",
		},
		{
			name:            "start line not integer",
			input:           "mode: set\nfoo.go:abc.1,2.2 1 1\n",
			wantErrContains: "start line not int",
		},
		{
			name:            "end missing dot",
			input:           "mode: set\nfoo.go:1.1,22 1 1\n",
			wantErrContains: "end missing '.'",
		},
		{
			name:            "end line not integer",
			input:           "mode: set\nfoo.go:1.1,abc.2 1 1\n",
			wantErrContains: "end line not int",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCoverProfile([]byte(tt.input))
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErrContains)
			}
		})
	}
}

func TestParseTestNames(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		prefixes []string
		want     []string
	}{
		{
			name:     "fail with timing stripped",
			input:    "--- FAIL: TestFoo (0.01s)\n",
			prefixes: []string{"--- FAIL: "},
			want:     []string{"TestFoo"},
		},
		{
			name:     "pass without timing",
			input:    "--- PASS: TestBar\n",
			prefixes: []string{"--- PASS: "},
			want:     []string{"TestBar"},
		},
		{
			name:     "non-matching lines are skipped",
			input:    "some random output\n",
			prefixes: []string{"--- FAIL: "},
			want:     nil,
		},
		{
			name:     "multiple prefixes collect all",
			input:    "--- FAIL: TestA (0.01s)\n--- PASS: TestB (0.00s)\n",
			prefixes: []string{"--- FAIL: ", "--- PASS: "},
			want:     []string{"TestA", "TestB"},
		},
		{
			name:     "timing with paren at position zero not stripped",
			input:    "--- PASS: TestC\n",
			prefixes: []string{"--- PASS: "},
			want:     []string{"TestC"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTestNames([]byte(tt.input), tt.prefixes...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseTestNames: want %v, got %v", tt.want, got)
			}
		})
	}
}

func TestBuildRunRegex(t *testing.T) {
	tests := []struct {
		names []string
		want  string
	}{
		{
			names: []string{"TestFoo"},
			want:  "^(TestFoo)$",
		},
		{
			names: []string{"TestA", "TestB"},
			want:  "^(TestA|TestB)$",
		},
		{
			names: []string{"Test.Plus"},
			want:  `^(Test\.Plus)$`,
		},
		{
			names: []string{"Test[Bracket]"},
			want:  `^(Test\[Bracket\])$`,
		},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.names, ","), func(t *testing.T) {
			got := buildRunRegex(tt.names)
			if got != tt.want {
				t.Errorf("buildRunRegex(%v): want %q, got %q", tt.names, tt.want, got)
			}
		})
	}
}

func TestFilterEnv(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		key  string
		want []string
	}{
		{
			name: "removes matching key",
			env:  []string{"A=1", "KANLY_MUTANT=5", "B=2"},
			key:  "KANLY_MUTANT",
			want: []string{"A=1", "B=2"},
		},
		{
			name: "keeps entries with similar prefix but different key",
			env:  []string{"KANLY_MUTANT=1", "KANLY_MUTANT_EXTRA=foo"},
			key:  "KANLY_MUTANT",
			want: []string{"KANLY_MUTANT_EXTRA=foo"},
		},
		{
			name: "no match passes through unchanged",
			env:  []string{"A=1", "B=2"},
			key:  "KANLY_MUTANT",
			want: []string{"A=1", "B=2"},
		},
		{
			name: "empty env returns empty",
			env:  []string{},
			key:  "KANLY_MUTANT",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterEnv(tt.env, tt.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("filterEnv: want %v, got %v", tt.want, got)
			}
		})
	}
}
