package runner

import (
	"reflect"
	"testing"
)

func TestSchemaLangFlags(t *testing.T) {
	tests := []struct {
		name      string
		goVersion string
		want      []string
	}{
		{"no module", "", nil},
		{"pre-generics", "1.16", []string{"-gcflags=example.com/p=-lang=go1.21"}},
		{"generics without 1.21 inference", "1.20", []string{"-gcflags=example.com/p=-lang=go1.21"}},
		{"exactly minimum", "1.21", nil},
		{"patch release", "1.21.4", nil},
		{"newer loopvar semantics must not be lowered", "1.22", nil},
		{"much newer", "1.25.0", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := schemaLangFlags("example.com/p", tt.goVersion)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("schemaLangFlags(%q) = %v, want %v", tt.goVersion, got, tt.want)
			}
		})
	}
}

func TestSchemaBuildFlags(t *testing.T) {
	if got, want := SchemaBuildFlags("example.com/p", "1.16"), []string{"-gcflags=example.com/p=-lang=go1.21"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SchemaBuildFlags = %v, want %v", got, want)
	}
}
