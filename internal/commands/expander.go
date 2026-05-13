package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type TemplateExpander struct {
	workDir string
}

func NewTemplateExpander(workDir string) *TemplateExpander {
	return &TemplateExpander{workDir: workDir}
}

// Expand processes the template with arguments and special syntax
func (e *TemplateExpander) Expand(ctx context.Context, template string, args []string) (string, error) {
	text := template

	// 1. Replace positional arguments $1, $2, ...
	text = e.replacePositionalArgs(text, args)

	// 2. Replace $ARGUMENTS
	text = strings.ReplaceAll(text, "$ARGUMENTS", strings.Join(args, " "))

	// 3. Replace {placeholder} and {placeholder|default}
	text = e.replacePlaceholders(text, args)

	// 4. Replace @filename (include file content)
	text = e.replaceFileReferences(text)

	// 5. Replace !`command` (shell execution)
	text, err := e.replaceShellCommands(ctx, text)
	if err != nil {
		return "", err
	}

	return text, nil
}

var positionalRegex = regexp.MustCompile(`\$(\d+)`)

func (e *TemplateExpander) replacePositionalArgs(text string, args []string) string {
	return positionalRegex.ReplaceAllStringFunc(text, func(match string) string {
		numStr := match[1:]
		num := 0
		fmt.Sscanf(numStr, "%d", &num)
		if num > 0 && num <= len(args) {
			return args[num-1]
		}
		return match
	})
}

var placeholderRegex = regexp.MustCompile(`\{([^}|]+)(?:\|([^}]+))?\}`)

func (e *TemplateExpander) replacePlaceholders(text string, args []string) string {
	return placeholderRegex.ReplaceAllStringFunc(text, func(match string) string {
		parts := placeholderRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		key := strings.TrimSpace(parts[1])
		defaultValue := ""
		if len(parts) > 2 {
			defaultValue = strings.TrimSpace(parts[2])
		}

		// Currently, we don't map placeholders to specific inputs via flags,
		// so we just substitute the default value if provided.
		if defaultValue != "" {
			return defaultValue
		}
		return "{" + key + "}"
	})
}

var fileRefRegex = regexp.MustCompile(`@([^\s@]+)`)

func (e *TemplateExpander) replaceFileReferences(text string) string {
	return fileRefRegex.ReplaceAllStringFunc(text, func(match string) string {
		filename := strings.TrimSpace(match[1:])

		fullPath := filepath.Join(e.workDir, filename)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Sprintf("[ERROR: Could not read file %s: %v]", filename, err)
		}

		return "\n\n--- File: " + filename + " ---\n" +
			string(content) +
			"\n--- End of " + filename + " ---\n\n"
	})
}

// Matches !`git log` syntax.
var shellCmdRegex = regexp.MustCompile(`!` + "`([^`]+)`")

func (e *TemplateExpander) replaceShellCommands(ctx context.Context, text string) (string, error) {
	var err error

	result := shellCmdRegex.ReplaceAllStringFunc(text, func(match string) string {
		cmdStr := strings.TrimSpace(match[2 : len(match)-1])

		cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		output, execErr := e.runShellCommand(cmdCtx, cmdStr)
		if execErr != nil {
			err = execErr // capture last error
			return fmt.Sprintf("[SHELL ERROR: %v]\nCommand: %s\nOutput: %s", execErr, cmdStr, output)
		}

		return output
	})

	return result, err
}

func (e *TemplateExpander) runShellCommand(ctx context.Context, cmdStr string) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	cmd.Dir = e.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stderr.String() + stdout.String(), err
	}

	return stdout.String(), nil
}
