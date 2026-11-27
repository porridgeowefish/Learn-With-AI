//go:build linux || darwin

package filesystem

import "os"

func replaceFile(from, to string) error {
	return os.Rename(from, to)
}
