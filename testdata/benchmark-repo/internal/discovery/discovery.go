package discovery

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

func Discover(root string, extensions []string) ([]string, error) {
	allowed := make(map[string]bool, len(extensions))
	for _, extension := range extensions {
		allowed[extension] = true
	}
	var files []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if entry.IsDir() {
			// Directory exclusions run before extension matching. .tmp is always
			// excluded, vendor is excluded at any depth, and other hidden
			// directories are excluded only when nested below the root.
			if parts[0] == ".tmp" || contains(parts, "vendor") {
				return filepath.SkipDir
			}
			if len(parts) > 1 && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if allowed[filepath.Ext(entry.Name())] {
			files = append(files, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func contains(parts []string, value string) bool {
	for _, part := range parts {
		if part == value {
			return true
		}
	}
	return false
}
