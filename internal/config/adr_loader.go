package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var adrFileRe = regexp.MustCompile(`^(\d{4})-.+\.md$`)

// ADR is a loaded Architecture Decision Record.
type ADR struct {
	ID          string   // e.g. "0001"
	Title       string
	Status      string   // Accepted, Superseded, Proposed, …
	Path        string
	Supersedes  []string
	SupersededBy string
	Body        string
}

// ADRLoader selects relevant accepted ADRs at an approved base revision.
type ADRLoader struct {
	DocsDir string // typically <repo>/docs/adr
}

// NewADRLoader roots at workDir/docs/adr.
func NewADRLoader(workDir string) *ADRLoader {
	return &ADRLoader{DocsDir: filepath.Join(workDir, "docs", "adr")}
}

// LoadAll reads numbered ADR files. Conflicts (two Accepted for same ID) error.
func (l *ADRLoader) LoadAll() ([]ADR, error) {
	entries, err := os.ReadDir(l.DocsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []ADR
	seen := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !adrFileRe.MatchString(e.Name()) {
			continue
		}
		path := filepath.Join(l.DocsDir, e.Name())
		adr, err := parseADR(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if prev, ok := seen[adr.ID]; ok && strings.EqualFold(adr.Status, "Accepted") && prev == "Accepted" {
			return nil, fmt.Errorf("ADR conflict: two Accepted records for %s", adr.ID)
		}
		if strings.EqualFold(adr.Status, "Accepted") {
			seen[adr.ID] = "Accepted"
		}
		out = append(out, adr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// SelectAccepted returns current accepted ADRs, following supersession links
// so superseded predecessors are excluded.
func (l *ADRLoader) SelectAccepted() ([]ADR, []string, error) {
	all, err := l.LoadAll()
	if err != nil {
		return nil, nil, err
	}
	superseded := map[string]bool{}
	byID := map[string]ADR{}
	for _, a := range all {
		byID[a.ID] = a
		for _, s := range a.Supersedes {
			superseded[normalizeADRID(s)] = true
		}
		if a.SupersededBy != "" {
			superseded[a.ID] = true
		}
	}
	var selected []ADR
	var loadedIDs []string
	for _, a := range all {
		if !strings.EqualFold(a.Status, "Accepted") {
			continue
		}
		if superseded[a.ID] {
			continue
		}
		selected = append(selected, a)
		loadedIDs = append(loadedIDs, a.ID)
	}
	return selected, loadedIDs, nil
}

func normalizeADRID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "ADR")
	s = strings.TrimPrefix(s, "adr")
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		s = fmt.Sprintf("%04s", s)
	}
	// keep leading digits
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			break
		}
	}
	return b.String()
}

func parseADR(path string) (ADR, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ADR{}, err
	}
	base := filepath.Base(path)
	m := adrFileRe.FindStringSubmatch(base)
	adr := ADR{Path: path, Body: string(b)}
	if len(m) == 2 {
		adr.ID = m[1]
	}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(line, "# "):
			adr.Title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
		case strings.HasPrefix(lower, "**status**:"):
			adr.Status = strings.TrimSpace(line[len("**Status**:"):])
			if adr.Status == "" {
				adr.Status = strings.TrimSpace(line[strings.Index(lower, ":")+1:])
			}
		case strings.HasPrefix(lower, "status:"):
			adr.Status = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		case strings.Contains(lower, "supersedes:"):
			rest := line[strings.Index(lower, "supersedes:")+len("supersedes:"):]
			for _, p := range strings.Split(rest, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					adr.Supersedes = append(adr.Supersedes, p)
				}
			}
		case strings.Contains(lower, "superseded by:"):
			rest := line[strings.Index(lower, "superseded by:")+len("superseded by:"):]
			adr.SupersededBy = strings.TrimSpace(rest)
		}
	}
	if adr.Status == "" {
		adr.Status = "Proposed"
	}
	return adr, sc.Err()
}
