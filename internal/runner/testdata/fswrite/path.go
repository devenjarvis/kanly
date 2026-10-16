// Package fswrite has mutants that make its test write into the package
// directory: dropping the `!` (or negating isEmpty's comparison) drops the
// directory from the path.
package fswrite

func Path(dir, name string) string {
	if !isEmpty(dir) {
		return dir + "/" + name
	}
	return name
}

func isEmpty(s string) bool {
	return len(s) == 0
}
