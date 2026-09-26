// Package exportfile writes sensitive directory exports without clobbering files accidentally.
package exportfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ValidatePath checks whether path is suitable for an export.
func ValidatePath(path string, force bool) error {
	if strings.TrimSpace(path) == "" || path == "-" {
		return errors.New("export file must be a file path")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect export file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("export file %q is a directory", path)
	}
	if !force {
		return fmt.Errorf("export file %q already exists; choose another path or allow replacement", path)
	}
	return nil
}

// WriteJSON atomically writes indented JSON with mode 0600.
func WriteJSON(path string, value any, force bool) error {
	if err := ValidatePath(path, force); err != nil {
		return err
	}

	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary export file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure temporary export file: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write export file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close export file: %w", err)
	}

	if force {
		if err := os.Rename(temporaryPath, path); err != nil {
			return fmt.Errorf("replace export file: %w", err)
		}
		return nil
	}
	if err := os.Link(temporaryPath, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("export file %q already exists; choose another path or allow replacement", path)
		}
		return fmt.Errorf("create export file: %w", err)
	}
	return nil
}
