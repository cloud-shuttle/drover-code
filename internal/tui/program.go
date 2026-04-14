package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/config"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/permissions"
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

	loop := agent.NewLoop(client, convoMgr, registry, eng, eventCh)
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

	model.SetRunFunc(func(input string) tea.Cmd {
		return runAgent(func() error {
			return loop.Run(ctx, input)
		})
	})
	model.SetCompactFn(func() error {
		return loop.CompactContext(ctx)
	})

	p.tea = tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
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
