package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type FileType int

const (
	FileTypeDockerfile FileType = iota
	FileTypeCompose
)

func DetectFileType(path string) FileType {
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml") {
		return FileTypeCompose
	}
	return FileTypeDockerfile
}

var skipDirs = map[string]bool{
	".git":         true,
	".claude":      true,
	"node_modules": true,
	"vendor":       true,
	".terraform":   true,
	"dist":         true,
	".next":        true,
}

func isTargetFile(name string) bool {
	lower := strings.ToLower(name)
	if lower == "dockerfile" {
		return true
	}
	if strings.HasPrefix(lower, "dockerfile.") && !strings.HasSuffix(lower, ".go") && !strings.HasSuffix(lower, ".md") {
		return true
	}
	if strings.HasPrefix(lower, "docker-compose") && (strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")) {
		return true
	}
	if lower == "compose.yml" || lower == "compose.yaml" {
		return true
	}
	return false
}

func FindFiles(filePath string, globPattern string) ([]string, error) {
	if filePath != "" {
		if _, err := os.Stat(filePath); err != nil {
			return nil, fmt.Errorf("file not found: %s", filePath)
		}
		return []string{filePath}, nil
	}
	if globPattern != "" {
		matches, err := doublestar.FilepathGlob(globPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern: %w", err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no files matched pattern: %s", globPattern)
		}
		return matches, nil
	}
	// Default: walk directory tree, skip heavy dirs
	var allMatches []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if isTargetFile(d.Name()) {
			allMatches = append(allMatches, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}
	if len(allMatches) == 0 {
		return nil, fmt.Errorf("no Dockerfiles or compose files found")
	}
	return allMatches, nil
}
