package main

import (
	"fmt"
	"strings"
)

// cliFlags holds global CLI options parsed before subsystems start.
type cliFlags struct {
	Headless    bool
	Prompt      string
	PromptFile        string
	ResultJSON        string
	CoordinatorRemote bool
	CloudMode         bool
	AcceptCmd         string
	Verbose           bool
}

// parseCLIFlags extracts known flags from argv. Unknown tokens are ignored so
// forward-compatible positional args can be added later.
func parseCLIFlags(argv []string) (cliFlags, error) {
	var f cliFlags
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "-v" || a == "--verbose":
			f.Verbose = true
		case a == "--headless":
			f.Headless = true
		case a == "--coordinator-remote":
			f.CoordinatorRemote = true
		case a == "--cloud":
			f.CloudMode = true
		case a == "-p" || a == "--prompt":
			if i+1 >= len(argv) {
				return f, fmt.Errorf("%s: value required", a)
			}
			i++
			f.Prompt = argv[i]
		case strings.HasPrefix(a, "--prompt="):
			f.Prompt = strings.TrimPrefix(a, "--prompt=")
		case strings.HasPrefix(a, "-p="):
			f.Prompt = strings.TrimPrefix(a, "-p=")
		case a == "--prompt-file":
			if i+1 >= len(argv) {
				return f, fmt.Errorf("%s: path required", a)
			}
			i++
			f.PromptFile = argv[i]
		case strings.HasPrefix(a, "--prompt-file="):
			f.PromptFile = strings.TrimPrefix(a, "--prompt-file=")
		case a == "--result-json":
			if i+1 >= len(argv) {
				return f, fmt.Errorf("%s: path required", a)
			}
			i++
			f.ResultJSON = argv[i]
		case strings.HasPrefix(a, "--result-json="):
			f.ResultJSON = strings.TrimPrefix(a, "--result-json=")
		case a == "--accept-cmd":
			if i+1 >= len(argv) {
				return f, fmt.Errorf("%s: command required", a)
			}
			i++
			f.AcceptCmd = argv[i]
		case strings.HasPrefix(a, "--accept-cmd="):
			f.AcceptCmd = strings.TrimPrefix(a, "--accept-cmd=")
		default:
			// ignore unknown
		}
	}
	return f, nil
}
