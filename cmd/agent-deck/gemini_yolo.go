package main

import (
	"fmt"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

func applyCLIYoloOverride(inst *session.Instance, enabled bool, explicit ...bool) error {
	if inst == nil || (!enabled && (len(explicit) == 0 || !explicit[0])) {
		return nil
	}
	tool := inst.Tool
	if session.IsCodexCompatible(tool) {
		tool = "codex"
	}
	switch tool {
	case "gemini":
		inst.SetGeminiYoloMode(enabled)
	case "codex":
		yolo := enabled
		opts := inst.GetCodexOptions()
		if opts == nil {
			opts = &session.CodexOptions{}
		}
		opts.YoloMode = &yolo
		if err := inst.SetCodexOptions(opts); err != nil {
			return err
		}
	case "hermes":
		opts := inst.GetHermesOptions()
		if opts == nil {
			opts = &session.HermesOptions{}
		}
		yolo := enabled
		opts.YoloMode = &yolo
		return inst.SetHermesOptions(opts)
	case "agy":
		opts := inst.GetAgyOptions()
		if opts == nil {
			userConfig, _ := session.LoadUserConfig()
			opts = session.NewAgyOptions(userConfig)
		}
		opts.DangerousMode = enabled
		return inst.SetAgyOptions(opts)
	default:
		return fmt.Errorf("--yolo only works with Gemini, Codex, Hermes or Antigravity (agy) sessions")
	}
	return nil
}
