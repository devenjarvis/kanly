package structfieldpkg

// Item is a small struct with mixed primitive and nilable fields.
type Item struct {
	Name  string
	Count int
	Tag   string
	Buf   []byte
	Owner *Item
}

// MakeKeyed constructs an Item with KEYED initializers — the canonical case
// this operator targets. Eligible value expressions:
//   Name:  name      → 1 (ident, string)
//   Count: count     → 1 (ident, int)
//   Tag:   getTag()  → 1 (call,  string)
//   Buf:   buf       → 1 (ident, []byte)
//   Owner: owner     → 1 (ident, *Item)
// Total: 5 candidates.
func MakeKeyed(name string, count int, buf []byte, owner *Item) Item {
	return Item{
		Name:  name,
		Count: count,
		Tag:   getTag(),
		Buf:   buf,
		Owner: owner,
	}
}

func getTag() string { return "tag" }

// SkippedShapes: each value is an AST shape return_zero excludes (BinaryExpr,
// UnaryExpr, BasicLit) or a literal nil ident. Expected: 0 candidates.
func SkippedShapes(a, b int) Item {
	return Item{
		Name:  "literal", // *ast.BasicLit — skipped
		Count: a + b,     // *ast.BinaryExpr — skipped
		Tag:   "x",       // *ast.BasicLit — skipped
		Buf:   nil,       // ident "nil" — skipped by returnZeroCandidate
		Owner: nil,       // ident "nil" — skipped
	}
}

// PositionalLit uses positional initialization — Elts are not KeyValueExpr,
// so this whole literal is out of scope. Expected: 0 candidates.
func PositionalLit() Item {
	return Item{"n", 1, "t", nil, nil}
}

// SliceLit: composite literal whose type is []int, not a struct. Expected: 0.
func SliceLit() []int {
	return []int{1, 2, 3}
}

// MapLit: composite literal of map type, not a struct. Expected: 0.
func MapLit() map[string]int {
	return map[string]int{"a": 1}
}

// Wrapper / NestedStruct: outer Wrapper struct's `Inner` field value is an
// inner Item composite literal. The inner CompositeLit isn't in
// return_zero's AST-shape skip list, but its static type is a non-nilable
// struct so returnZeroMutant returns "" — the outer Inner is therefore not
// a candidate. The inner Item's only KV (Name: "x") is a BasicLit (skipped).
// The outer Label gets a single candidate. Expected: 1 candidate.
type Wrapper struct {
	Label string
	Inner Item
}

func NestedStruct() Wrapper {
	return Wrapper{
		Label: getLabel(),
		Inner: Item{Name: "x"},
	}
}

func getLabel() string { return "lbl" }
