// Package config loads and merges drover-code settings from the three-level
// hierarchy: global (~/.drover/settings.json) → project (.drover/settings.json)
// → local (.drover/settings.local.json).
//
// Legacy Claude Code paths (~/.claude and .claude at each level) are still read
// when present; drover paths override claude paths at the same tier.
//
// It also walks the directory tree for CLAUDE.md files and concatenates them
// into a system-prompt injection.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudshuttle/drover-code/internal/integrations/sqlforge"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// CommandConfig holds JSON/YAML command definitions when not using Markdown files.
type CommandConfig struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Template    string `json:"template,omitempty" yaml:"template,omitempty"`
	Agent       string `json:"agent,omitempty" yaml:"agent,omitempty"`
	Model       string `json:"model,omitempty" yaml:"model,omitempty"`
	RiskTier    int    `json:"risk_tier,omitempty" yaml:"risk_tier,omitempty"`
	Subtask     bool   `json:"subtask,omitempty" yaml:"subtask,omitempty"`
}

// Settings is the merged configuration from all three levels.
type Settings struct {
	Model                      string                   `json:"model,omitempty"`
	PermissionMode             string                   `json:"permissionMode,omitempty"`
	AllowedTools               []string                 `json:"allowedTools,omitempty"`
	DeniedTools                []string                 `json:"deniedTools,omitempty"`
	MaxTokens                  int                      `json:"maxTokens,omitempty"`
	CoordinatorMode            bool                     `json:"coordinatorMode,omitempty"`
	CoordinatorRemote          bool                     `json:"coordinatorRemote,omitempty"`
	AcceptCmd                  string                   `json:"acceptCmd,omitempty"`
	Verbose                    bool                     `json:"verbose,omitempty"`
	DreamEnabled               bool                     `json:"dreamEnabled,omitempty"`
	UndercoverMode             *bool                    `json:"undercoverMode,omitempty"`
	Env                        map[string]string        `json:"env,omitempty"`
	Commands                   map[string]CommandConfig `json:"commands,omitempty"`
	ContextLimitEstimate       int                      `json:"contextLimitEstimate,omitempty"`
	CharsPerTokenEstimate      int                      `json:"charsPerTokenEstimate,omitempty"`
	ProjectMarkdownMaxBytes    int                      `json:"projectMarkdownMaxBytes,omitempty"`
	ProjectMarkdownMaxFiles    int                      `json:"projectMarkdownMaxFiles,omitempty"`
	ProjectMarkdownIgnoreGlobs []string                 `json:"projectMarkdownIgnoreGlobs,omitempty"`
	DisableAutoCompaction      bool                     `json:"disableAutoCompaction,omitempty"`
	DreamMaxRetentionEntries   int                      `json:"dreamMaxRetentionEntries,omitempty"`
	DreamMaxRetentionAgeDays   int                      `json:"dreamMaxRetentionAgeDays,omitempty"`
}
// Loader manages settings loading and CLAUDE.md injection.
type Loader struct {
	mu         sync.RWMutex
	merged     Settings
	systemInj  string
	globalDir  string
	projectDir string
	workDir    string
	onChange   []func(Settings)
}

// NewLoader creates a Loader rooted at workDir.
func NewLoader(workDir string) *Loader {
	home, _ := os.UserHomeDir()
	return &Loader{
		workDir:    workDir,
		globalDir:  filepath.Join(home, ".drover"),
		projectDir: filepath.Join(workDir, ".drover"),
	}
}

func settingsLoadPaths(home, workDir string) []string {
	return []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".drover", "settings.json"),
		filepath.Join(workDir, ".claude", "settings.json"),
		filepath.Join(workDir, ".drover", "settings.json"),
		filepath.Join(workDir, ".claude", "settings.local.json"),
		filepath.Join(workDir, ".drover", "settings.local.json"),
	}
}

// Load reads and merges all settings files.
func (l *Loader) Load() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var merged Settings
	home, _ := os.UserHomeDir()
	paths := settingsLoadPaths(home, l.workDir)

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("config: read %s: %w", p, err)
		}
		var s Settings
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("config: parse %s: %w", p, err)
		}
		mergeInto(&merged, s)
	}

	l.merged = merged // loadProjectMarkdown must not take l.mu: Load holds the write lock here.
	l.systemInj = l.loadProjectMarkdown(merged)
	if proj, ok := sqlforge.FindProject(l.workDir); ok {
		l.systemInj += sqlforge.SystemPrompt(proj)
	}
	return nil
}

// Get returns a snapshot of the current merged settings.
func (l *Loader) Get() Settings {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.merged
}

// SystemInjection returns CLAUDE.md content to prepend to the system prompt.
func (l *Loader) SystemInjection() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.systemInj
}

// OnChange registers a callback invoked when settings reload.
func (l *Loader) OnChange(fn func(Settings)) {
	l.mu.Lock()
	l.onChange = append(l.onChange, fn)
	l.mu.Unlock()
}

// Save writes a delta to the project-level settings.json.
func (l *Loader) Save(delta Settings) error {
	path := filepath.Join(l.projectDir, "settings.json")
	if err := os.MkdirAll(l.projectDir, 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	var existing Settings
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	mergeInto(&existing, delta)
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("config: write: %w", err)
	}
	return os.Rename(tmp, path)
}

// loadProjectMarkdown walks from workDir upward collecting CLAUDE.md files.
// merged is the settings snapshot to read caps from (caller must not hold l.mu).
func (l *Loader) loadProjectMarkdown(merged Settings) string {
	maxBytes := merged.ProjectMarkdownMaxBytes
	maxFiles := merged.ProjectMarkdownMaxFiles

	if s := strings.TrimSpace(os.Getenv("DROVER_CODE_MAX_PROJECT_MARKDOWN_BYTES")); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			maxBytes = v
		}
	}
	if s := strings.TrimSpace(os.Getenv("DROVER_CODE_PROJECT_MARKDOWN_MAX_FILES")); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			maxFiles = v
		}
	}

	var files []string
	dir := l.workDir
	home, _ := os.UserHomeDir()

	// Global CLAUDE.md first (lowest priority).
	if global := filepath.Join(home, ".claude", "CLAUDE.md"); fileExists(global) {
		files = append(files, global)
	}

	// Walk upward from workDir.
	for {
		if c := filepath.Join(dir, "CLAUDE.md"); fileExists(c) {
			files = append(files, c)
		}
		if d := filepath.Join(dir, ".drover.md"); fileExists(d) {
			files = append(files, d)
		}
		if a := filepath.Join(dir, "AGENTS.md"); fileExists(a) {
			files = append(files, a)
		}
		parent := filepath.Dir(dir)
		if parent == dir || dir == home {
			break
		}
		dir = parent
	}

	if maxFiles > 0 && len(files) > maxFiles {
		files = files[:maxFiles]
	}

	ignore := merged.ProjectMarkdownIgnoreGlobs
	if len(ignore) > 0 {
		filtered := files[:0]
		for _, f := range files {
			rel, _ := filepath.Rel(l.workDir, f)
			if rel == "" {
				rel = f
			}
			if markdownPathMatchesIgnore(rel, ignore) {
				continue
			}
			filtered = append(filtered, f)
		}
		files = filtered
	}

	if len(files) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("# Project context (from .drover.md / AGENTS.md / CLAUDE.md)\n\n")
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(l.workDir, f)
		if rel == "" {
			rel = f
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", rel, strings.TrimSpace(string(data)))
	}
	out := b.String()
	if maxBytes > 0 && len(out) > maxBytes {
		out = truncateUTF8Prefix(out, maxBytes)
		out += "\n\n_(Project markdown injection truncated: maxBytes exceeded)_\n"
	}
	return out
}

func truncateUTF8Prefix(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// mergeInto merges non-zero fields from src into dst.
func mergeInto(dst *Settings, src Settings) {
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.PermissionMode != "" {
		dst.PermissionMode = src.PermissionMode
	}
	if len(src.AllowedTools) > 0 {
		dst.AllowedTools = append(dst.AllowedTools, src.AllowedTools...)
	}
	if len(src.DeniedTools) > 0 {
		dst.DeniedTools = append(dst.DeniedTools, src.DeniedTools...)
	}
	if src.MaxTokens != 0 {
		dst.MaxTokens = src.MaxTokens
	}
	if src.ContextLimitEstimate != 0 {
		dst.ContextLimitEstimate = src.ContextLimitEstimate
	}
	if src.CharsPerTokenEstimate != 0 {
		dst.CharsPerTokenEstimate = src.CharsPerTokenEstimate
	}
	if src.ProjectMarkdownMaxBytes != 0 {
		dst.ProjectMarkdownMaxBytes = src.ProjectMarkdownMaxBytes
	}
	if src.ProjectMarkdownMaxFiles != 0 {
		dst.ProjectMarkdownMaxFiles = src.ProjectMarkdownMaxFiles
	}
	if len(src.ProjectMarkdownIgnoreGlobs) > 0 {
		dst.ProjectMarkdownIgnoreGlobs = append(dst.ProjectMarkdownIgnoreGlobs, src.ProjectMarkdownIgnoreGlobs...)
	}
	if src.DisableAutoCompaction {
		dst.DisableAutoCompaction = true
	}
	if src.CoordinatorMode {
		dst.CoordinatorMode = true
	}
	if src.CoordinatorRemote {
		dst.CoordinatorRemote = true
	}
	if src.AcceptCmd != "" {
		dst.AcceptCmd = src.AcceptCmd
	}
	if src.Verbose {
		dst.Verbose = true
	}
	if src.DreamEnabled {
		dst.DreamEnabled = true
	}
	if src.DreamMaxRetentionEntries != 0 {
		dst.DreamMaxRetentionEntries = src.DreamMaxRetentionEntries
	}
	if src.DreamMaxRetentionAgeDays != 0 {
		dst.DreamMaxRetentionAgeDays = src.DreamMaxRetentionAgeDays
	}
	if src.UndercoverMode != nil {
		dst.UndercoverMode = src.UndercoverMode
	}
	for k, v := range src.Env {
		if dst.Env == nil {
			dst.Env = make(map[string]string)
		}
		dst.Env[k] = v
	}
	for k, v := range src.Commands {
		if dst.Commands == nil {
			dst.Commands = make(map[string]CommandConfig)
		}
		dst.Commands[k] = v
	}
}

// EffectiveDisableAutoCompaction is true when settings or
// DROVER_CODE_DISABLE_AUTO_COMPACTION requests skipping automatic context compaction.
func EffectiveDisableAutoCompaction(s Settings) bool {
	if s.DisableAutoCompaction {
		return true
	}
	v := strings.ToLower(strings.TrimSpace(os.Getenv("DROVER_CODE_DISABLE_AUTO_COMPACTION")))
	switch v {
	case "1", "true", "yes", "on", "y":
		return true
	default:
		return false
	}
}

// EffectiveDreamEnabled is true when settings or
// DROVER_CODE_DREAM_ENABLED requests enabling Dream memory.
func EffectiveDreamEnabled(s Settings) bool {
	if s.DreamEnabled {
		return true
	}
	v := strings.ToLower(strings.TrimSpace(os.Getenv("DROVER_CODE_DREAM_ENABLED")))
	switch v {
	case "1", "true", "yes", "on", "y":
		return true
	default:
		return false
	}
}
