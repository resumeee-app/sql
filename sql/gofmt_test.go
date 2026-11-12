package sql_test

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoFmtCompliance verifies that all Go source files in the repository are formatted with gofmt.
func TestGoFmtCompliance(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate go.mod from working directory")
		}
		dir = parent
	}

	var unformatted []string
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "web" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		formatted, fmtErr := format.Source(src)
		if fmtErr != nil {
			t.Errorf("failed to format %s: %v", path, fmtErr)
			return nil
		}
		if !bytes.Equal(src, formatted) {
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				rel = path
			}
			unformatted = append(unformatted, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repository for gofmt check: %v", err)
	}
	if len(unformatted) > 0 {
		t.Fatalf("the following Go files are not formatted with gofmt (run `gofmt -w .`):\n  %s", strings.Join(unformatted, "\n  "))
	}
}
