package slicerangepkg

// TwoIndex: both Low and High set with int variables. Expected: 4 candidates (2 bounds × ±1).
func TwoIndex(s []int, lo, hi int) []int { return s[lo:hi] }

// LowOnly: only Low set. Expected: 2 candidates (Low ±1).
func LowOnly(s []int, lo int) []int { return s[lo:] }

// HighOnly: only High set. Expected: 2 candidates (High ±1).
func HighOnly(s []int, hi int) []int { return s[:hi] }

// FullBounds: three-index slice with Low, High, Max. Expected: 6 candidates (3 bounds × ±1).
func FullBounds(s []int, lo, hi, max int) []int { return s[lo:hi:max] }

// AllNil: open-ended slice s[:]. Expected: 0 candidates (no bounds present).
func AllNil(s []int) []int { return s[:] }

// ConstBounds: constant bounds are excluded (purely-constant indices skipped). Expected: 0 candidates.
func ConstBounds(s []int) []int { return s[0:3] }

// Int32Bounds: sized-int bound, accepted under the underlying-integer policy. Expected: 2 candidates.
func Int32Bounds(s []int, hi int32) []int { return s[:hi] }
