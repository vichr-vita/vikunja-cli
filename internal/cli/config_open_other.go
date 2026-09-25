//go:build !unix

package cli

import "os"

func openConfigFile(path string) (*os.File, error) {
	return os.Open(path)
}
