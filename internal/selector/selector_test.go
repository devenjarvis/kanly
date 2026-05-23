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
		// Exact rest extraction: colon at index 3, rest must start at index 4 not 2.
		{
			name: "short package name exact func extraction",
			args: []string{"./p:Z"},
			want: []Spec{{Pattern: "./p", Funcs: []string{"Z"}}},
		},
		// Wrapped error must contain the selector arg (catches empty-format-string mutation).
		{
			name:      "wrapped error contains selector arg",
			args:      []string{"./pkg:Foo,"},
			wantError: `selector "./pkg:Foo,"`,
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
		// *T with no dot — exercises the dot < 0 → return false path.
		{"*Foo", "(*Foo).Bar", false},
		{"*Foo", "Foo", false},
		// *T with single-char type: verifies method is extracted from dot+1, not dot-1.
		{"*T.M", "(*T).M", true},
		{"*T.M", "(*T).N", false},
		// T.Method with single-char receiver (dot==1): verifies dot > 0 boundary.
		{"T.M", "(T).M", true},
		{"T.M", "(*T).M", true},
		{"T.M", "(T).N", false},
		// dot == 0 (.Method): verifies dot > 0 correctly excludes this case.
		{".Method", "().Method", false},
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

func TestSplitOutsideParens(t *testing.T) {
	cases := []struct {
		s    string
		sep  byte
		want int
	}{
		{"./pkg:Foo", ':', 5},
		{"noSep", ':', -1},
		// sep is the only character
		{":", ':', 0},
		// sep inside parens — must not split
		{"(T:Foo)", ':', -1},
		// open then close then sep — depth tracking must decrement on ')'
		{"(T):Foo", ':', 3},
		// extra ')' at depth 0 is ignored, sep still found after it
		{"(T)):Foo", ':', 4},
		// nested parens, sep after both close — depth must track correctly
		{"((T)):Foo", ':', 5},
	}
	for _, tc := range cases {
		got := splitOutsideParens(tc.s, tc.sep)
		if got != tc.want {
			t.Errorf("splitOutsideParens(%q, %q) = %d, want %d", tc.s, tc.sep, got, tc.want)
		}
	}
}

func TestSplitFuncList(t *testing.T) {
	cases := []struct {
		input   string
		want    []string
		wantErr string
	}{
		{"Foo", []string{"Foo"}, ""},
		{"Foo,Bar", []string{"Foo", "Bar"}, ""},
		{"(T).M,(*U).N", []string{"(T).M", "(*U).N"}, ""},
		// unbalanced ')' at depth 0 — exercises the depth==0 guard and error branch
		{"Foo),Bar", nil, "unbalanced ')'"},
		// unbalanced ')' error message must be exact
		{")", nil, "unbalanced ')'"},
		// empty function name between commas
		{"Foo,,Bar", nil, "empty function name"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := splitFuncList(tc.input)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !equalStrings(got, tc.want) {
				t.Errorf("= %v, want %v", got, tc.want)
			}
		})
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
