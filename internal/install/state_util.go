package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// countFilesUnder returns the number of regular files under root (recursive).
func countFilesUnder(root string) (int, error) {
	if root == "" {
		return 0, nil
	}
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("count files under: %w", err)
		}
		if d.IsDir() {
			return nil
		}
		n++
		return nil
	})
	return n, err
}
