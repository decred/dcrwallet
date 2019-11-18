package assets

import (
	"path/filepath"
	"runtime"
)

var basepath string

func init() {
	_, goFilename, _, _ := runtime.Caller(0)
	basepath = filepath.Dir(goFilename)
}

// Path returns the absolute filepath of a file in the dcrwallet main module,
// relative to the assets package.
//
// For example, to access the filepath of a sample-dcrwallet.conf file from the
// root of the main module, call Path("../sample-dcrwallet.conf").
//
// This function is only usable when built without -trimpath and on the host the
// Go program was compiled on.
func Path(asset string) string {
	return filepath.Join(basepath, asset)
}
