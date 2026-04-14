package dream

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Retention limits how many dream entries are kept. Zero values mean unlimited.
type Retention struct {
	MaxEntries int // keep at most this many newest entries; 0 = no cap
	MaxAgeDays int // delete entries older than this many whole days; 0 = no age limit
}

// Active reports whether any pruning rule is enabled.
func (r Retention) Active() bool {
	return r.MaxEntries > 0 || r.MaxAgeDays > 0
}

// ApplyEnvOverrides replaces limits from DROVER_CODE_DREAM_MAX_ENTRIES and
// DROVER_CODE_DREAM_MAX_AGE_DAYS when set (non-empty). Use "0" for unlimited.
func (r *Retention) ApplyEnvOverrides() {
	if s := strings.TrimSpace(os.Getenv("DROVER_CODE_DREAM_MAX_ENTRIES")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			r.MaxEntries = n
		}
	}
	if s := strings.TrimSpace(os.Getenv("DROVER_CODE_DREAM_MAX_AGE_DAYS")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			r.MaxAgeDays = n
		}
	}
}

func (r Retention) minTimestamp() (cutoff time.Time, ok bool) {
	if r.MaxAgeDays <= 0 {
		return time.Time{}, false
	}
	return time.Now().UTC().Add(-time.Duration(r.MaxAgeDays) * 24 * time.Hour), true
}
