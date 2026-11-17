package tui

import (
	"strings"

	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/permissions"
)

const planModePrompt = "\n\nPlan mode is on. Do not call write_file, edit_file, bash, or any tool that changes the workspace. read_file, list_directory, file_info, glob, grep, explore, and write_plan still run. Shift+Tab leaves plan mode."

func applyPlanModePrompt(mgr *convo.Manager, on bool) {
	if mgr == nil {
		return
	}
	cur := strings.TrimSuffix(mgr.SystemPrompt(), planModePrompt)
	if on {
		cur += planModePrompt
	}
	mgr.SetSystemPrompt(cur)
}

func wirePlanMode(m *Model, eng *permissions.Engine, mgr *convo.Manager) {
	if m == nil || eng == nil {
		return
	}
	restore := eng.Mode()
	if restore == permissions.ModePlan {
		restore = permissions.ModeDefault
		m.setPlanOn(true)
		applyPlanModePrompt(mgr, true)
	}
	m.planToggle = func(on bool) {
		if on {
			if eng.Mode() != permissions.ModePlan {
				restore = eng.Mode()
			}
			eng.SetMode(permissions.ModePlan)
			return
		}
		next := restore
		if next == permissions.ModePlan {
			next = permissions.ModeDefault
		}
		eng.SetMode(next)
	}
}

func (m *Model) setPlanOn(on bool) {
	m.planOn = on
	if m != nil && m.StatusBar != nil {
		m.StatusBar.PlanMode = on
	}
}

func (m *Model) togglePlanMode() {
	if m == nil {
		return
	}
	m.setPlanOn(!m.planOn)
	if m.planToggle != nil {
		m.planToggle(m.planOn)
	}
	applyPlanModePrompt(m.convoMgr, m.planOn)
	if m.planOn {
		m.appendLocalInfo("Plan mode on. Edits and bash are refused. Shift+Tab leaves plan mode.")
		return
	}
	m.appendLocalInfo("Plan mode off.")
}
