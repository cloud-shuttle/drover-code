package convo

import (
	"fmt"

	"github.com/cloudshuttle/drover-code/internal/api"
)

// CompactionCutPoint returns the slice index where summarisation should start:
// messages msgs[:cut] are replaced by a summary user message; msgs[cut:] is kept.
//
// minTail is the minimum number of messages to retain from the end. The cut may
// move earlier (retaining more messages) so the suffix never begins with a user
// turn that contains tool_result blocks — those must follow an assistant message
// with matching tool_use blocks, and a synthetic summary user message does not
// satisfy that requirement (Anthropic 400: unexpected tool_use_id in tool_result).
func CompactionCutPoint(msgs []api.Message, minTail int) (cut int, err error) {
	if len(msgs) <= minTail {
		return 0, fmt.Errorf("compaction: need more than %d messages in history", minTail)
	}
	cut = len(msgs) - minTail
	for cut > 0 && tailStartsWithOrphanToolResults(msgs[cut:]) {
		cut--
	}
	if cut == 0 {
		return 0, fmt.Errorf("compaction: cannot trim prefix without breaking tool result pairing; try /clear")
	}
	return cut, nil
}

func tailStartsWithOrphanToolResults(tail []api.Message) bool {
	if len(tail) == 0 {
		return false
	}
	if tail[0].Role != api.RoleUser {
		return false
	}
	for _, b := range tail[0].Content {
		if _, ok := b.(api.ToolResultBlock); ok {
			return true
		}
	}
	return false
}
