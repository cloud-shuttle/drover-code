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
						loop.SetDriver(agent.NewAnthropicInferenceDriver(api.NewClient(os.Getenv("ANTHROPIC_API_KEY"), cmdDef.Model)))
					}
				} else if !strings.Contains(err.Error(), "not found") {
					if strings.Contains(err.Error(), "Drover Guard") {
						return fmt.Errorf("🚨 Command Blocked by Governance Policy: %v", err)
					}
					return fmt.Errorf("command error: %v", err)
				}
			}
			return loop.Run(runCtx, input)
		})
	})
	model.SetCompactFn(func() error {
		return loop.CompactContext(ctx)
	})

	p.tea = tea.NewProgram(
		model,
		tea.WithAltScreen(),
	)

	return p
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
