package runner

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseTestDependents(t *testing.T) {
	out := []byte("m/app\t/m/app\tm/lib\n" +
		"m/lib\t/m/lib\t\n" +
		"m/util\t/m/util\t\n" +
		"m/notests\t/m/notests\tm/lib\n" +
		// lib.test is listed before app.test, so results must be sorted.
		"m/lib.test\t/m/lib\tm/lib [m/lib.test] m/util\n" +
		// lib appears plain and recompiled for app's test: one dependent.
		"m/app.test\t/m/app\tfmt m/app [m/app.test] m/lib m/lib [m/app.test] m/util os\n" +
		"m/app [m/app.test]\t/m/app\tm/lib\n" +
		"m/util.test\t/m/util\tm/util [m/util.test] m/util\n" +
		"other/ext.test\t/x\tm/lib\n" +
		"malformed line\n")
	got := parseTestDependents(out)
	want := map[string][]Dependent{
		"m/lib":  {{ImportPath: "m/app", Dir: "/m/app"}},
		"m/util": {{ImportPath: "m/app", Dir: "/m/app"}, {ImportPath: "m/lib", Dir: "/m/lib"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseTestDependents =\n%v\nwant\n%v", got, want)
	}
}

func TestTestDependentsFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	got, err := TestDependents(context.Background(), "testdata/crosspkg")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range got["crosspkg/lib"] {
		names = append(names, d.ImportPath)
	}
	if want := []string{"crosspkg/app", "crosspkg/brokentest", "crosspkg/mainfail", "crosspkg/orderdep", "crosspkg/reader"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("dependents of crosspkg/lib = %v, want %v", names, want)
	}
	if len(got["crosspkg/other"]) != 0 {
		t.Errorf("crosspkg/other has dependents: %v", got["crosspkg/other"])
	}
	if deps := got["crosspkg/app"]; len(deps) != 1 || deps[0].ImportPath != "crosspkg/orderdep" {
		t.Errorf("dependents of crosspkg/app = %v, want just crosspkg/orderdep", deps)
	}

	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, "go.mod"), []byte("not a go.mod\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TestDependents(context.Background(), broken); err == nil || !strings.Contains(err.Error(), "go list -test") {
		t.Errorf("TestDependents with a broken go.mod: err = %v, want a go list error", err)
	}
}
