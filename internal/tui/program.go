package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/commands"
	"github.com/cloudshuttle/drover-code/internal/config"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/telemetry"
	"github.com/cloudshuttle/drover-code/internal/tools"
	"github.com/cloudshuttle/drover-code/internal/tui/commandpalette"
	liveregion "github.com/cloudshuttle/drover-code/internal/tui/components/liveregion"
	"github.com/cloudshuttle/drover-code/internal/tui/core"
	"github.com/cloudshuttle/drover-code/internal/tui/styles"
)

type Program struct {
	tea     *tea.Program
	model   *Model
	loop    *agent.Loop
	eventCh chan agent.Event
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewProgram(
	baseCtx context.Context,
	client *api.Client,
	convoMgr *convo.Manager,
	registry *tools.Registry,
	modelName string,
	settings config.Settings,
	workDir string,
	cmdExec *commands.Executor,
	themeSaveFn func(mode, palette string) error,
) *Program {
	eventCh := make(chan agent.Event, 512)
	ctx, cancel := context.WithCancel(baseCtx)
	permitFn := makePermitFn(ctx, eventCh)

	mode := permissions.ParseMode(settings.PermissionMode)
	// Plan mode only makes sense with an interactive UI. If anything routes through
	// here, it's interactive, so we keep the configured mode.
	eng := permissions.NewEngine(
		mode,
		settings.AllowedTools,
		settings.DeniedTools,
		filepath.Join(workDir, ".claude", "permissions.json"),
		permitFn,
	)

	driver := agent.NewAnthropicInferenceDriver(client)
	executor := agent.NewDefaultToolExecutor(registry, eng, eventCh)
	loop := agent.NewLoop(driver, convoMgr, executor, registry, eventCh)
	config.ApplyAgentLoopOptions(loop, settings)
	userName := strings.TrimSpace(os.Getenv("USER"))
	if userName == "" {
		userName = strings.TrimSpace(os.Getenv("USERNAME"))
	}
	if userName == "" {
		userName = "user"
	}
	hostName, _ := os.Hostname()
	if hostName == "" {
		hostName = "host"
	}
	model := New(eventCh, modelName, workDir, userName, hostName)
	model.SetConversation(convoMgr)

	p := &Program{
		loop:    loop,
		model:   model,
		eventCh: eventCh,
		ctx:     ctx,
		cancel:  cancel,
	}

	if cmdExec != nil {
		cmds := cmdExec.GetRegistry().List()
		var names, descs []string
		for _, c := range cmds {
			names = append(names, c.Name)
			descs = append(descs, c.Description)
		}
		model.RegisterCustomCommands(names, descs)

		// Rich Command Palette integration using the new extensibility APIs.
		//
		// 1. Use a CommandProvider so the list of custom commands is dynamic
		//    (reflects commands added/removed at runtime) and carries rich
		//    metadata (Category + RiskLevel derived from RiskTier).
		model.RegisterPaletteProvider(func() []commandpalette.Command {
			var paletteCmds []commandpalette.Command
			for _, c := range cmdExec.GetRegistry().List() {
				risk := "normal"
				switch {
				case c.RiskTier >= 2:
					risk = "high"
				case c.RiskTier >= 1:
					risk = "caution"
				}

				actionKey := "custom:" + c.Name

				paletteCmds = append(paletteCmds, commandpalette.Command{
					Name:        c.Name,
					Description: c.Description,
					Category:    "Custom",
					RiskLevel:   risk,
					ActionKey:   actionKey, // mark as semantic action
				})
			}
			return paletteCmds
		})

		// 2. Register handlers so that selecting a custom command from the
		//    palette executes it *directly* (instead of just injecting text).
		//    This is the power of RegisterPaletteActionHandler.
		for _, c := range cmdExec.GetRegistry().List() {
			name := c.Name
			actionKey := "custom:" + name

			model.RegisterPaletteActionHandler(actionKey, func(key string) tea.Cmd {
				cmdToRun := "/" + name

				// Update the visible input (nice UX feedback)
				model.InputArea.SetValue(cmdToRun + " ")
				model.InputArea.CursorEnd()

				// Execute directly via the normal run path.
				// This is equivalent to the user typing the command and pressing Enter.
				if model.runFunc != nil {
					return model.runFunc(cmdToRun)
				}
				return nil
			})
		}
	}

	model.SetRunFunc(func(input string) tea.Cmd {
		runCtx, runCancel := context.WithCancel(ctx)
		model.SetRunCancel(runCancel)

		return runAgent(func() error {
			defer runCancel()
			if cmdExec != nil && strings.HasPrefix(strings.TrimSpace(input), "/") {
				parts := strings.Fields(strings.TrimSpace(input))
				cmdName := strings.TrimPrefix(parts[0], "/")

				if cmdName == "commands" {
					if len(parts) > 1 && parts[1] == "init" {
						wd, _ := os.Getwd()
						if err := commands.Init(wd); err != nil {
							convoMgr.Append(api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: fmt.Sprintf("Error initializing commands: %v", err)}}))
						} else {
							convoMgr.Append(api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: "✅ Successfully initialized starter commands in .drover/commands/\nRun `/commands list` to see them."}}))
						}
						return nil
					}

					// default to list
					cmds := cmdExec.GetRegistry().List()
					var b strings.Builder
					if len(cmds) == 0 {
						b.WriteString("No custom commands loaded.\nRun `/commands init` to create starter commands.")
					} else {
						b.WriteString("Available Custom Commands\n=========================\n\n")
						for _, c := range cmds {
							extra := []string{}
							if c.Agent != "" && c.Agent != "default" {
								extra = append(extra, "agent:"+c.Agent)
							}
							if c.RiskTier > 0 {
								extra = append(extra, fmt.Sprintf("risk:%d", c.RiskTier))
							}
							extraStr := ""
							if len(extra) > 0 {
								extraStr = "  [" + strings.Join(extra, ", ") + "]"
							}
							fmt.Fprintf(&b, "- `/%s` - %s%s\n", c.Name, c.Description, extraStr)
						}
						b.WriteString("\nTip: Run `/commands init` to add more starter commands.")
					}
					convoMgr.Append(api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: b.String()}}))
					return nil
				} else if cmdName == "score" {
					if len(parts) < 2 {
						convoMgr.Append(api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: "Usage: `/score <value> [comment...]`"}}))
						return nil
					}
					val, err := strconv.ParseFloat(parts[1], 64)
					if err != nil {
						convoMgr.Append(api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: fmt.Sprintf("Invalid score: %v", err)}}))
						return nil
					}
					comment := strings.Join(parts[2:], " ")
					traceID := loop.LastTraceID()
					if traceID == "" {
						convoMgr.Append(api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: "No previous trace to score."}}))
						return nil
					}
					telemetry.TracerFrom(ctx).Score(traceID, "user_feedback", val, telemetry.ScoreSourceHuman, comment)
					convoMgr.Append(api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: fmt.Sprintf("✅ Score %v recorded for trace `%s`", val, traceID)}}))
					return nil
				}

				if expanded, cmdDef, err := cmdExec.EvaluateAndExpand(runCtx, cmdName, parts[1:]); err == nil {
					input = expanded
					if cmdDef.Model != "" {
						loop.SetDriver(agent.NewAnthropicInferenceDriver(client.WithModel(cmdDef.Model)))
					}
				} else if !strings.Contains(err.Error(), "not found") {
					if strings.Contains(err.Error(), "Drover Guard") {
						return fmt.Errorf("🚨 Command Blocked by Governance Policy: %v", err)
					}
					return fmt.Errorf("command error: %v", err)
				}
			}
			if input == "__internal_resume__" {
				// Resume: continue the agent loop with existing convo state, no new user message.
				return loop.Run(runCtx, "")
			}
			return loop.Run(runCtx, input)
		})
	})
	model.SetCompactFn(func() error {
		return loop.CompactContext(ctx)
	})
	if themeSaveFn != nil {
		model.SetThemeSaveFn(themeSaveFn)
	}

	// Visual theme picker in the Command Palette (Theme System)
	model.RegisterPaletteProvider(func() []commandpalette.Command {
		var cmds []commandpalette.Command
		for name := range styles.NamedPalettes {
			for _, mode := range []styles.ThemeMode{styles.ThemeLight, styles.ThemeDark, styles.ThemeAuto} {
				displayMode := string(mode)
				if mode == styles.ThemeAuto {
					displayMode = "auto"
				}
				// Human-friendly name (Title() will suppress the leading / for these)
				displayName := name
				if name == "default" {
					displayName = "Default"
				} else if name == "high-contrast" {
					displayName = "High-Contrast"
				}
				modeTitle := displayMode
				if displayMode == "light" {
					modeTitle = "Light"
				} else if displayMode == "dark" {
					modeTitle = "Dark"
				} else {
					modeTitle = "Auto"
				}
				cmds = append(cmds, commandpalette.Command{
					Name:        displayName + " " + modeTitle,
					Description: "Switch UI to the " + displayName + " palette (" + displayMode + " mode). Custom colors from settings are preserved.",
					ActionKey:   "theme:" + name + ":" + string(mode),
					Category:    "Theme",
				})
			}
		}
		return cmds
	})

	// Handle theme actions from the palette
	model.RegisterPaletteActionHandler("theme:", func(key string) tea.Cmd {
		// key is like "theme:default:dark"
		parts := strings.Split(key, ":")
		if len(parts) >= 3 {
			name := parts[1]
			modeStr := parts[2]
			mode := styles.ThemeMode(modeStr)
			styles.SetNamedPalette(name, mode)

			// Persist if possible
			if model.themeSaveFn != nil {
				_ = model.themeSaveFn(modeStr, name)
			}
			model.relayout()
		}
		return nil
	})

	// Filter out terminal control sequences (especially OSC color queries like
	// "]11rgb:..." or "]11;rgb:...") that can leak into the input stream on
	// startup or with certain terminals. Without this, they end up in the
	// textarea and get sent to the model (or just look like garbage in the prompt).
	p.tea = tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
			if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyRunes {
				s := string(key.Runes)
				clean := liveregion.StripTerminalOSCResponses(s)
				clean = liveregion.StripCursorPositionReports(clean)
				clean = liveregion.StripStandaloneBackslashLines(clean)
				clean = liveregion.StripBareRGBTriplets(clean)
				if clean == "" {
					return nil // drop the message entirely
				}
				if clean != s {
					key.Runes = []rune(clean)
					return key
				}
			}
			return msg
		}),
	)

	return p
}

// Note shows an operator message in the transcript before the first prompt.
func (p *Program) Note(text string) {
	if p == nil || p.model == nil || p.model.HistoryView == nil || strings.TrimSpace(text) == "" {
		return
	}
	p.model.HistoryView.AppendTurn(core.RenderedTurn{Role: "system", Content: text})
}

func (p *Program) Run() error {
	defer p.cancel()
	if _, err := p.tea.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func makePermitFn(ctx context.Context, eventCh chan<- agent.Event) tools.PermissionFunc {
	return func(callCtx context.Context, req tools.PermissionRequest) tools.Decision {
		respCh := make(chan agent.PermissionDecision, 1)

		select {
		case eventCh <- agent.PermissionRequestEvent{
			ToolName:   req.ToolName,
			Input:      req.Input,
			Summary:    req.Summary,
			DecisionCh: respCh,
		}:
		case <-callCtx.Done():
			return tools.Deny
		case <-ctx.Done():
			return tools.Deny
		}

		select {
		case d := <-respCh:
			switch d {
			case agent.PermAllow:
				return tools.Allow
			case agent.PermAlwaysAllow:
				return tools.AlwaysAllow
			default:
				return tools.Deny
			}
		case <-callCtx.Done():
			return tools.Deny
		case <-ctx.Done():
			return tools.Deny
		}
	}
}

func (p *Program) Stderr() *os.File {
	return os.Stderr
}
