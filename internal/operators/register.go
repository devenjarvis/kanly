package operators

import "github.com/devenjarvis/cauldron/internal/mutation"

func init() {
	mutation.Register(IntArith{})
	mutation.Register(IntCmpBoundary{})
	mutation.Register(IntCmpNegate{})
}
