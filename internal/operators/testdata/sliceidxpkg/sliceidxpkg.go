package sliceidxpkg

// SliceIdx indexes a slice with an int variable. Expected: 2 candidates (+1, -1).
func SliceIdx(s []int, i int) int { return s[i] }

// ArrayIdx indexes an array with an int variable. Expected: 2 candidates.
func ArrayIdx(a [4]int, i int) int { return a[i] }

// StringByte indexes a string with an int variable. Expected: 2 candidates.
func StringByte(s string, i int) byte { return s[i] }

// MapIntKey indexes a map[int]string with an int variable. Expected: 2 candidates.
func MapIntKey(m map[int]string, k int) string { return m[k] }

// MapStrKey uses a string key. Expected: 0 candidates (key type not exactly int).
func MapStrKey(m map[string]int) int { return m["key"] }

// MyInt is a named int type. Under the underlying-integer policy named ints
// are mutated like plain int.
type MyInt int

// MapNamedKey uses a named-int key. Expected: 2 candidates.
func MapNamedKey(m map[MyInt]int, k MyInt) int { return m[k] }

// ConstIdx uses a constant index. Expected: 0 candidates (constants excluded).
func ConstIdx(s []int) int { return s[0] }

// ConstExprIdx uses a constant arithmetic expression. Expected: 0 candidates.
func ConstExprIdx(s []int) int { return s[1+1] }
