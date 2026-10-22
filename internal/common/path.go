package common

import (
	"path/filepath"
	"strings"
)

func PathWithin(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
