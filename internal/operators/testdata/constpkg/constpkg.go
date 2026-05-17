package constpkg

const x = 1 + 2 // both operands are untyped constants — should be skipped

func Add(a, b int) int { return a + b } // non-constant — should be found
