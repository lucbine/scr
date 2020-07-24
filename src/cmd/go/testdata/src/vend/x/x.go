package x

import (
	_ "p"
	_ "q"

	_ "r"

	_ "vend/dir1"

	// not vendored
	_ "vend/dir1/dir2"
)

// vendored
