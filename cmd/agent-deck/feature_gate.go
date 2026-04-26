package main

import (
	"errors"
	"fmt"

	"github.com/asheshgoplani/agent-deck/internal/platform"
)

const nativeWindowsSupportedFeatures = "Supported native Windows features: TUI startup, session create/start, interactive attach, send input, status detection, restart, remote SSH workflows, web UI / browser bridge, and Codex sessions."

func nativeWindowsDeferredFeatureError(feature, fallback string) error {
	return nativeWindowsDeferredFeatureErrorForPlatform(platform.Detect(), feature, fallback)
}

func nativeWindowsDeferredFeatureErrorForPlatform(p platform.Platform, feature, fallback string) error {
	if p != platform.PlatformWindows {
		return nil
	}

	msg := fmt.Sprintf("%s is not available on native Windows yet.", feature)
	if fallback != "" {
		msg += " " + fallback
	}
	msg += " " + nativeWindowsSupportedFeatures
	return errors.New(msg)
}
