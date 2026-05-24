// Package sqlforge detects Drover SQLForge data projects in the agent workspace.
package sqlforge

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const manifestName = "sqlforge.yml"

// Project describes a workspace-bound SQLForge data project.
type Project struct {
	Root               string
	ManifestPath       string
	DefaultEnvironment string
}

// FindProject walks upward from workDir for sqlforge.yml (ADR 0002: workspace-bound).
func FindProject(workDir string) (Project, bool) {
	dir := workDir
	for {
		manifest := filepath.Join(dir, manifestName)
		if fileExists(manifest) {
			return Project{
				Root:               dir,
				ManifestPath:       manifest,
				DefaultEnvironment: readDefaultEnvironment(manifest),
			}, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return Project{}, false
}

func readDefaultEnvironment(manifestPath string) string {
	f, err := os.Open(manifestPath)
	if err != nil {
		return "prod"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "default_environment:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "default_environment:"))
			v = strings.Trim(v, `"'`)
			if v != "" {
				return v
			}
		}
	}
	return "prod"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
