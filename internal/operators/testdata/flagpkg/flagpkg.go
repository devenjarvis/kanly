package flagpkg

import "flag"

type list []string

func (l *list) String() string     { return "" }
func (l *list) Set(s string) error { *l = append(*l, s); return nil }

// describe is not package flag: its usage argument is ordinary data.
func describe(name, usage string) string { return name + ": " + usage }

func Parse(args []string) (string, error) {
	level := flag.String("level", "info", "log level")
	fs := flag.NewFlagSet("tool", flag.ContinueOnError)
	var out string
	fs.StringVar(&out, "out", "-", "where to write")
	var tags list
	fs.Var(&tags, "tag", "a tag; repeatable")
	fs.Func("mode", "how to run", func(string) error { return nil })
	_ = describe("x", "explained")
	return *level + out, fs.Parse(args)
}
