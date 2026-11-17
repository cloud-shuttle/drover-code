package warden

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// bead is one line of policies.jsonl. Fields match the bead file drover-warden
// writes. Unknown fields are ignored, so a newer bead still loads.
type bead struct {
	ID                string   `json:"id"`
	Type              string   `json:"type"`
	Rule              string   `json:"rule"`
	Action            string   `json:"action"`
	Tools             []string `json:"tools"`
	DangerousArgs     []string `json:"dangerous_args"`
	ProtectedPaths    []string `json:"protected_paths"`
	ForbiddenFiles    []string `json:"forbidden_files"`
	Patterns          []string `json:"patterns"`
	DeniedTopics      []string `json:"denied_topics"`
	ForbiddenPatterns []string `json:"forbidden_patterns"`
	Forbidden         []string `json:"forbidden"`

	patterns  []*regexp.Regexp
	forbidden []*regexp.Regexp
}

// Engine evaluates a policies.jsonl directory.
type Engine struct {
	beads []bead
}

// Load reads policies.jsonl from dir.
func Load(dir string) (*Engine, error) {
	f, err := os.Open(filepath.Join(dir, "policies.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var beads []bead
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var b bead
		if err := json.Unmarshal([]byte(line), &b); err != nil {
			return nil, err
		}
		b.patterns = compilePatterns(b.Patterns)
		b.forbidden = compilePatterns(append(append([]string{}, b.ForbiddenPatterns...), b.Forbidden...))
		beads = append(beads, b)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return &Engine{beads: beads}, nil
}

func compilePatterns(pats []string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, p := range pats {
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			continue
		}
		out = append(out, re)
	}
	return out
}

func (e *Engine) CheckAction(req *GuardRequest) Decision {
	start := time.Now()
	if req == nil || req.ToolCall == nil {
		return allow("action", "action_passed", start)
	}
	tool := req.ToolCall.ToolName
	cmd, path, files := actionFields(req.ToolCall.Args)
	for _, b := range e.beads {
		if b.Type != "action" && b.Type != "global" {
			continue
		}
		if !b.matchesTool(tool) {
			continue
		}
		if !b.actionMatch(cmd, path, files) {
			continue
		}
		return b.decide("action", start)
	}
	return allow("action", "action_passed", start)
}

func (e *Engine) CheckInput(req *GuardRequest) Decision {
	start := time.Now()
	text := ""
	if req != nil {
		text = req.Input
	}
	for _, b := range e.beads {
		if b.Type != "input" && b.Type != "global" {
			continue
		}
		if b.textMatch(text) {
			return b.decide("input", start)
		}
	}
	return allow("input", "input_passed", start)
}

func (e *Engine) CheckOutput(req *GuardRequest) Decision {
	start := time.Now()
	text := ""
	if req != nil {
		text = outputText(req.Output)
	}
	for _, b := range e.beads {
		if b.Type != "output" && b.Type != "global" {
			continue
		}
		if b.outputMatch(text) {
			return b.decide("output", start)
		}
	}
	return allow("output", "output_passed", start)
}

func (b bead) matchesTool(name string) bool {
	if len(b.Tools) == 0 {
		return true
	}
	for _, t := range b.Tools {
		if t == name {
			return true
		}
	}
	return false
}

func (b bead) actionMatch(cmd, path string, files []string) bool {
	for _, arg := range b.DangerousArgs {
		if arg != "" && strings.Contains(cmd, arg) {
			return true
		}
	}
	for _, p := range b.ProtectedPaths {
		if p != "" && (strings.HasPrefix(path, p) || strings.Contains(path, p)) {
			return true
		}
	}
	for _, file := range files {
		for _, forbidden := range b.ForbiddenFiles {
			clean := strings.ReplaceAll(forbidden, "*", "")
			if clean != "" && strings.Contains(file, clean) {
				return true
			}
		}
	}
	return false
}

func (b bead) textMatch(text string) bool {
	for _, re := range b.patterns {
		if re.MatchString(text) {
			return true
		}
	}
	lower := strings.ToLower(text)
	for _, topic := range b.DeniedTopics {
		if topic != "" && strings.Contains(lower, strings.ToLower(topic)) {
			return true
		}
	}
	return false
}

func (b bead) outputMatch(text string) bool {
	if b.textMatch(text) {
		return true
	}
	for _, re := range b.forbidden {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func (b bead) decide(stage string, start time.Time) Decision {
	allowed := b.Action == "" || b.Action == "passed" || b.Action == "warn" || b.Action == "suggest"
	reason := b.Rule
	if reason == "" {
		reason = b.ID
	}
	result := b.Action
	if result == "" {
		result = "matched"
	}
	return Decision{
		Allowed: allowed,
		Result: GuardResult{
			BeadID:    b.ID,
			Result:    result,
			Reason:    reason,
			LatencyMS: time.Since(start).Milliseconds(),
		},
	}
}

func allow(stage, reason string, start time.Time) Decision {
	_ = stage
	return Decision{
		Allowed: true,
		Result: GuardResult{
			Result:    "passed",
			Reason:    reason,
			LatencyMS: time.Since(start).Milliseconds(),
		},
	}
}

func actionFields(args map[string]any) (cmd, path string, files []string) {
	if args == nil {
		return "", "", nil
	}
	if c, ok := args["command"].(string); ok {
		cmd = c
	}
	if p, ok := args["path"].(string); ok {
		path = p
	}
	switch fl := args["files"].(type) {
	case []string:
		files = fl
	case []any:
		for _, f := range fl {
			if s, ok := f.(string); ok {
				files = append(files, s)
			}
		}
	}
	return cmd, path, files
}

func outputText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
