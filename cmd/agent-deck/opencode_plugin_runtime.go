package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const openCodeRuntimePluginFileName = "agentdeck-session-binding.js"

func handleOpenCodePluginInstall() {
	pluginPath, installed, err := ensureOpenCodeRuntimePluginInstalled()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error installing OpenCode plugin: %v\n", err)
		os.Exit(1)
	}
	if installed {
		fmt.Println("OpenCode agent-deck plugin installed successfully.")
	} else {
		fmt.Println("OpenCode agent-deck plugin is already installed.")
	}
	fmt.Printf("Plugin: %s\n", pluginPath)
}

func handleOpenCodePluginUninstall() {
	pluginPath, removed, err := removeOpenCodeRuntimePlugin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error removing OpenCode plugin: %v\n", err)
		os.Exit(1)
	}
	if removed {
		fmt.Println("OpenCode agent-deck plugin removed successfully.")
		fmt.Printf("Plugin: %s\n", pluginPath)
		return
	}
	fmt.Println("No agent-deck OpenCode plugin found to remove.")
	fmt.Printf("Plugin: %s\n", pluginPath)
}

func handleOpenCodePluginStatus() {
	status, err := getOpenCodeRuntimePluginStatus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking OpenCode plugin status: %v\n", err)
		os.Exit(1)
	}

	if status.Installed {
		fmt.Println("Status: INSTALLED")
	} else {
		fmt.Println("Status: NOT INSTALLED")
		fmt.Println("Run 'agent-deck opencode-plugin install' to install.")
	}
	fmt.Printf("Plugin: %s\n", status.Path)
}

type openCodeRuntimePluginStatus struct {
	Installed bool
	Path      string
}

func getOpenCodeRuntimePluginStatus() (*openCodeRuntimePluginStatus, error) {
	pluginPath, err := getOpenCodeRuntimePluginPath()
	if err != nil {
		return nil, err
	}
	_, statErr := os.Stat(pluginPath)
	if statErr == nil {
		return &openCodeRuntimePluginStatus{Installed: true, Path: pluginPath}, nil
	}
	if os.IsNotExist(statErr) {
		return &openCodeRuntimePluginStatus{Installed: false, Path: pluginPath}, nil
	}
	return nil, statErr
}

func ensureOpenCodeRuntimePluginInstalled() (string, bool, error) {
	pluginPath, err := getOpenCodeRuntimePluginPath()
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o755); err != nil {
		return "", false, err
	}

	desired := renderOpenCodeRuntimePlugin()
	current, err := os.ReadFile(pluginPath)
	if err == nil && string(current) == desired {
		return pluginPath, false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}

	if err := os.WriteFile(pluginPath, []byte(desired), 0o644); err != nil {
		return "", false, err
	}
	return pluginPath, true, nil
}

func removeOpenCodeRuntimePlugin() (string, bool, error) {
	pluginPath, err := getOpenCodeRuntimePluginPath()
	if err != nil {
		return "", false, err
	}
	if err := os.Remove(pluginPath); err != nil {
		if os.IsNotExist(err) {
			return pluginPath, false, nil
		}
		return "", false, err
	}
	return pluginPath, true, nil
}

func getOpenCodeRuntimePluginPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "plugins", openCodeRuntimePluginFileName), nil
}

func renderOpenCodeRuntimePlugin() string {
	return `import { mkdir, rename, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

function clean(value) {
  return typeof value === "string" ? value.trim() : "";
}

function deepLookup(obj, segments) {
  let current = obj;
  for (const segment of segments) {
    if (!current || typeof current !== "object") {
      return "";
    }
    current = current[segment];
  }
  return clean(current);
}

function extractSession(event) {
  const candidates = [
    [["sessionID"], "sessionID"],
    [["session_id"], "session_id"],
    [["session", "id"], "session.id"],
    [["session", "sessionID"], "session.sessionID"],
    [["properties", "sessionID"], "properties.sessionID"],
    [["properties", "session_id"], "properties.session_id"],
  ];
  for (const [segments, field] of candidates) {
    const value = deepLookup(event, segments);
    if (value) {
      return { value, field };
    }
  }
  return { value: "", field: "" };
}

function resolveStatus(eventType, event) {
  const normalized = clean(eventType).toLowerCase();
  if (normalized === "session.status") {
    const statusType = clean(event?.properties?.status?.type).toLowerCase();
    if (statusType === "busy" || statusType === "running") {
      return "running";
    }
    if (statusType === "idle" || statusType === "waiting") {
      return "waiting";
    }
  }
  if (normalized === "session.idle") {
    return "waiting";
  }
  if (normalized === "server.instance.disposed" || normalized === "session.ended") {
    return "dead";
  }
  return "waiting";
}

function hooksDir() {
  return path.join(os.homedir(), ".agent-deck", "hooks");
}

async function writeHookFiles(instanceID, status, eventType, sessionID) {
  const dir = hooksDir();
  const statusPath = path.join(dir, instanceID + ".json");
  const sidPath = path.join(dir, instanceID + ".sid");
  await mkdir(dir, { recursive: true });

  const payload = {
    status,
    session_id: sessionID,
    event: eventType,
    ts: Math.floor(Date.now() / 1000),
  };
  const nonce = String(Date.now()) + "-" + Math.random().toString(16).slice(2);
  const tmpStatusPath = statusPath + "." + nonce + ".tmp";
  await writeFile(tmpStatusPath, JSON.stringify(payload));
  await rename(tmpStatusPath, statusPath);

  if (sessionID) {
    const tmpSIDPath = sidPath + "." + nonce + ".tmp";
    await writeFile(tmpSIDPath, sessionID);
    await rename(tmpSIDPath, sidPath);
    return;
  }
  await rm(sidPath, { force: true });
}

export const AgentDeckSessionBinding = async () => {
  const instanceID = clean(process.env.AGENTDECK_INSTANCE_ID);
  if (!instanceID) {
    return {};
  }

  return {
    event: async ({ event }) => {
      const eventType = clean(event?.type);
      const session = extractSession(event);
      if (!eventType || !session.value) {
        return;
      }
      await writeHookFiles(instanceID, resolveStatus(eventType, event), eventType, session.value);
    },
  };
};
`
}

func printOpenCodePluginUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: agent-deck opencode-plugin <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Manage the OpenCode agent-deck session-binding plugin.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  install       Install or upgrade the OpenCode session-binding plugin")
	fmt.Fprintln(w, "  uninstall     Remove the OpenCode session-binding plugin")
	fmt.Fprintln(w, "  status        Show OpenCode plugin install status")
	fmt.Fprintln(w, "  phase0-proof  Run the Phase 0 proof spike for OpenCode session binding")
}
