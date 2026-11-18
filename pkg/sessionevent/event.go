// Package sessionevent forwards to github.com/cloud-shuttle/drover-libs/pkg/sessionevent,
// which now owns the issue-run session JSONL schema (ADR-003).
//
// Deprecated: import github.com/cloud-shuttle/drover-libs/pkg/sessionevent
// directly. This package stays so existing importers keep building; it will
// be removed after the next drover-code minor release.
package sessionevent

import libs "github.com/cloud-shuttle/drover-libs/pkg/sessionevent"

// Version is the schema version written into every event.
const Version = libs.Version

// Event types in schema version 1.
const (
	TypeStarted           = libs.TypeStarted
	TypeStatus            = libs.TypeStatus
	TypeMessage           = libs.TypeMessage
	TypeToolRequested     = libs.TypeToolRequested
	TypeToolFinished      = libs.TypeToolFinished
	TypeApprovalRequested = libs.TypeApprovalRequested
	TypeApprovalResolved  = libs.TypeApprovalResolved
	TypeArtifactWritten   = libs.TypeArtifactWritten
	TypeLinked            = libs.TypeLinked
)

// Event is one session-event line. It is the drover-libs type, so values
// pass between this package and drover-libs without conversion.
type Event = libs.Event

// New builds an event stamped at the current UTC time.
func New(typ, sessionID string, data map[string]any) Event { return libs.New(typ, sessionID, data) }

// Append writes one event as a single JSON line, creating parent directories.
func Append(path string, ev Event) error { return libs.Append(path, ev) }

// ReadAll parses every line of the file at path.
func ReadAll(path string) ([]Event, error) { return libs.ReadAll(path) }
