package selector

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		want      []Spec
		wantError string
	}{
		{
			name: "bare package pattern",
			args: []string{"./internal/foo"},
			want: []Spec{{Pattern: "./internal/foo"}},
		},
		{
			name: "glob pattern",
			args: []string{"./..."},
			want: []Spec{{Pattern: "./..."}},
		},
		{
			name: "single func",
			args: []string{"./pkg:Foo"},
			want: []Spec{{Pattern: "./pkg", Funcs: []string{"Foo"}}},
		},
		{
			name: "multi func",
			args: []string{"./pkg:Foo,Bar"},
			want: []Spec{{Pattern: "./pkg", Funcs: []string{"Foo", "Bar"}}},
		},
		{
			name: "method with pointer receiver",
			args: []string{"./pkg:(*Server).Handle"},
			want: []Spec{{Pattern: "./pkg", Funcs: []string{"(*Server).Handle"}}},
		},
		{
			name: "comma inside parens is not a separator",
			args: []string{"./pkg:(*Server).Handle,(Foo).Bar"},
			want: []Spec{{Pattern: "./pkg", Funcs: []string{"(*Server).Handle", "(Foo).Bar"}}},
		},
		{
			name: "method dotted form",
			args: []string{"./pkg:Server.Handle"},
			want: []Spec{{Pattern: "./pkg", Funcs: []string{"Server.Handle"}}},
		},
		{
			name: "multiple positional args",
			args: []string{"./a:Foo", "./b"},
			want: []Spec{
				{Pattern: "./a", Funcs: []string{"Foo"}},
				{Pattern: "./b"},
			},
		},
		{
			name:      "glob with func filter is rejected",
			args:      []string{"./...:Foo"},
			wantError: "function filter requires a single package",
		},
		{
			name:      "empty pattern before colon",
			args:      []string{":Foo"},
			wantError: "empty package pattern",
		},
		{
			name:      "empty func list after colon",
			args:      []string{"./pkg:"},
			wantError: "empty function list",
		},
		{
			name:      "empty func name in list",
			args:      []string{"./pkg:Foo,"},
			wantError: "empty function name",
		},
		{
			name:      "unbalanced paren",
			args:      []string{"./pkg:(Foo.Bar"},
			wantError: "unbalanced",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.args)
			if tc.wantError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantError)
				}
				if !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("specs: want %d, got %d", len(tc.want), len(got))
			}
			for i := range got {
				if got[i].Pattern != tc.want[i].Pattern {
					t.Errorf("[%d].Pattern: want %q, got %q", i, tc.want[i].Pattern, got[i].Pattern)
				}
				if !equalStrings(got[i].Funcs, tc.want[i].Funcs) {
					t.Errorf("[%d].Funcs: want %v, got %v", i, tc.want[i].Funcs, got[i].Funcs)
				}
			}
		})
	}
}

func TestMatchFunc(t *testing.T) {
	cases := []struct {
		user      string
		canonical string
		match     bool
	}{
		{"Foo", "Foo", true},
		{"Foo", "(Foo).Bar", false},
		{"Foo", "Bar", false},
		{"Server.Handle", "(Server).Handle", true},
		{"Server.Handle", "(*Server).Handle", true},
		{"Server.Handle", "Handle", false},
		{"Server.Handle", "(Server).Other", false},
		{"*Server.Handle", "(*Server).Handle", true},
		{"*Server.Handle", "(Server).Handle", false},
		{"(Server).Handle", "(Server).Handle", true},
		{"(Server).Handle", "(*Server).Handle", false},
		{"(*Server).Handle", "(*Server).Handle", true},
		{"(*Server).Handle", "(Server).Handle", false},
	}
	for _, tc := range cases {
		got := MatchFunc(tc.user, tc.canonical)
		if got != tc.match {
			t.Errorf("MatchFunc(%q, %q) = %v, want %v", tc.user, tc.canonical, got, tc.match)
		}
	}
}

func TestAnyMatch(t *testing.T) {
	entries := []string{"Foo", "Server.Handle"}
	cases := []struct {
		canonical string
		want      bool
	}{
		{"Foo", true},
		{"(Server).Handle", true},
		{"(*Server).Handle", true},
		{"Bar", false},
		{"(Other).Method", false},
	}
	for _, tc := range cases {
		got := AnyMatch(entries, tc.canonical)
		if got != tc.want {
			t.Errorf("AnyMatch(%v, %q) = %v, want %v", entries, tc.canonical, got, tc.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
