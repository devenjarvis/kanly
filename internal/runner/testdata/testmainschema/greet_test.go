package testmainschema

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	Greet()
	if !strings.HasSuffix(LastCaller, ".Greet") {
		fmt.Println("record called from", LastCaller)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestGreet(t *testing.T) { Greet() }
