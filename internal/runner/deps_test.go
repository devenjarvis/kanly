package runner

import (
	"context"
	"reflect"
	"testing"
)

func TestParseTestDependents(t *testing.T) {
	out := []byte("m/app\t/m/app\tm/lib\n" +
		"m/lib\t/m/lib\t\n" +
		"m/util\t/m/util\t\n" +
		"m/notests\t/m/notests\tm/lib\n" +
		"m/app.test\t/m/app\tfmt m/app [m/app.test] m/lib m/util os\n" +
		"m/app [m/app.test]\t/m/app\tm/lib\n" +
		"m/lib.test\t/m/lib\tm/lib [m/lib.test] m/util\n" +
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
	if want := []string{"crosspkg/app", "crosspkg/mainfail"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("dependents of crosspkg/lib = %v, want %v", names, want)
	}
	if len(got["crosspkg/other"]) != 0 || len(got["crosspkg/app"]) != 0 {
		t.Errorf("unexpected dependents: %v", got)
	}
}
