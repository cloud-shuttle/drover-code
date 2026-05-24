package ukc

import "github.com/cloudshuttle/drover-code/pkg/workercontract/workspace"

// DefaultWorkspaceLimits returns the default workspace upload size limits.
// It is a convenience shim so callers that import only the ukc package do not
// need a separate import of pkg/workercontract/workspace.
func DefaultWorkspaceLimits() workspace.Limits {
	return workspace.DefaultLimits()
}
