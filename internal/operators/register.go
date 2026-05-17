package operators

import "github.com/devenjarvis/kanly/internal/mutation"

func init() {
	mutation.Register(IntArith{})
	mutation.Register(IntCmpBoundary{})
	mutation.Register(IntCmpNegate{})
	mutation.Register(BoolLogic{})
	mutation.Register(BoolNot{})
	mutation.Register(ErrReturnNil{})
	mutation.Register(CallDelete{})
	mutation.Register(StringLiteral{})
	mutation.Register(SliceIndex{})
}
