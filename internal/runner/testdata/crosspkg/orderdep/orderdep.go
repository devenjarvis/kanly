// Package orderdep imports app, so it depends on both app and lib.
package orderdep

import "crosspkg/app"

// Area is app.Area.
var Area = app.Area
