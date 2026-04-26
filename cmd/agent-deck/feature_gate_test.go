package main

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/platform"
)

func TestNativeWindowsDeferredFeatureErrorForPlatform_Windows(t *testing.T) {
	err := nativeWindowsDeferredFeatureErrorForPlatform(
		platform.PlatformWindows,
		"Git worktree workflows",
		"Use macOS/Linux/WSL for worktree mode.",
	)
	if err == nil {
		t.Fatal("expected error for native Windows")
	}

	got := err.Error()
	for _, want := range []string{
		"Git worktree workflows is not available on native Windows yet.",
		"Supported native Windows features:",
		"worktree mode",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("error %q does not contain %q", got, want)
		}
	}
}

func TestNativeWindowsDeferredFeatureErrorForPlatform_NonWindows(t *testing.T) {
	for _, p := range []platform.Platform{
		platform.PlatformLinux,
		platform.PlatformMacOS,
		platform.PlatformWSL2,
	} {
		if err := nativeWindowsDeferredFeatureErrorForPlatform(p, "feature", "fallback"); err != nil {
			t.Fatalf("expected no error for %s, got %v", p, err)
		}
	}
}

func TestNativeWindowsSupportedFeaturesIncludesRemoteSSH(t *testing.T) {
	if !strings.Contains(nativeWindowsSupportedFeatures, "remote SSH workflows") {
		t.Fatalf("nativeWindowsSupportedFeatures = %q, want to include remote SSH workflows", nativeWindowsSupportedFeatures)
	}
}

func TestNativeWindowsSupportedFeaturesIncludesWebBridge(t *testing.T) {
	if !strings.Contains(nativeWindowsSupportedFeatures, "web UI / browser bridge") {
		t.Fatalf("nativeWindowsSupportedFeatures = %q, want to include web UI / browser bridge", nativeWindowsSupportedFeatures)
	}
}
