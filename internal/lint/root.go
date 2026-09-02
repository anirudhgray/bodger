package lint

import (
	"fmt"
	"os"
	"path/filepath"
)

// repoRoot returns the repository root by walking up from the working
// directory to the go.mod. Shared by every check in this package that
// needs a stable, working-directory-independent starting point for a
// full-repo scan (the import-graph and banned-symbol checks, issue #9,
// and the vocabulary check, issue #11) — vocab.go named its own copy of
// this vocabRepoRoot to leave room for exactly this extraction.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above the working directory")
		}
		dir = parent
	}
}
