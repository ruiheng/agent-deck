package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const codexNotifyMarkerBegin = "# BEGIN AGENTDECK CODEX NOTIFY"
const codexNotifyMarkerEnd = "# END AGENTDECK CODEX NOTIFY"
const codexNotifyLine = `notify = ["agent-deck", "codex-notify"]`

const codexLifecycleDescription = "Agent Deck Codex lifecycle status hooks."
const codexLifecycleCommand = "agent-deck codex-notify"

// Codex lifecycle edges are advisory observations. They improve the common
// running/waiting path while the existing notify and pane/process fallbacks
// remain responsible for stale, blocked, or continued turns.
var codexLifecycleEvents = []string{"UserPromptSubmit", "Stop"}

var codexNotifyTableRe = regexp.MustCompile(`(?m)^\s*\[notify\]\s*$`)
var codexNotifyKeyRe = regexp.MustCompile(`(?m)^\s*notify\s*=`)
var codexNotifyExactRe = regexp.MustCompile(`(?m)^\s*notify\s*=\s*\[\s*["']agent-deck["']\s*,\s*["']codex-notify["']\s*\]\s*$`)
var codexNotifyExactLineRe = regexp.MustCompile(`^\s*notify\s*=\s*\[\s*["']agent-deck["']\s*,\s*["']codex-notify["']\s*\]\s*$`)
var codexLegacyNotifyProgramLineRe = regexp.MustCompile(`(?i)^\s*program\s*=\s*\[\s*["']agent-deck["']\s*,\s*["']codex-notify["']\s*\]\s*$`)

type codexNotifyPayload struct {
	HookEventName string                     `json:"hook_event_name"`
	Type          string                     `json:"type"`
	Event         string                     `json:"event"`
	Method        string                     `json:"method"`
	SessionID     string                     `json:"session_id"`
	ThreadID      string                     `json:"thread_id"`
	ThreadIDDash  string                     `json:"thread-id"`
	Params        map[string]json.RawMessage `json:"params"`
	Payload       map[string]json.RawMessage `json:"payload"`
}

func mapCodexNotifyToStatus(event string) string {
	e := strings.ToLower(strings.TrimSpace(event))
	if e == "" {
		return ""
	}

	switch e {
	case "userpromptsubmit":
		return "running"
	case "stop":
		return "waiting"
	case "thread.started", "thread/started", "thread-started",
		"session.configured", "session/configured", "session-configured":
		return "waiting"
	case "agent-turn-complete", "agent-turn-completed", "turn/completed", "turn-completed", "turn.completed",
		"turn/complete", "turn-complete", "turn.complete", "turn/failed", "turn-failed", "turn.failed",
		"turn/aborted", "turn-aborted", "turn.aborted", "turn/cancelled", "turn-cancelled", "turn.cancelled",
		"turn/canceled", "turn-canceled", "turn.canceled":
		return "waiting"
	case "agent-turn-start", "agent-turn-started", "turn/started", "turn-started", "turn.started":
		return "running"
	default:
		canon := strings.NewReplacer(".", "/", "-", "/", "_", "/").Replace(e)
		if strings.Contains(canon, "thread/started") || strings.Contains(canon, "session/configured") {
			return "waiting"
		}
		if strings.Contains(canon, "turn") && (strings.Contains(canon, "complete") ||
			strings.Contains(canon, "fail") ||
			strings.Contains(canon, "abort") ||
			strings.Contains(canon, "cancel")) {
			return "waiting"
		}
		if strings.Contains(e, "turn") && strings.Contains(e, "complete") {
			return "waiting"
		}
		if strings.Contains(canon, "turn") && strings.Contains(canon, "start") {
			return "running"
		}
		return ""
	}
}

func decodeStringField(raw map[string]json.RawMessage, keys ...string) string {
	if len(raw) == 0 {
		return ""
	}
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || len(value) == 0 {
			continue
		}
		var str string
		if err := json.Unmarshal(value, &str); err == nil {
			str = strings.TrimSpace(str)
			if str != "" {
				return str
			}
		}
	}
	return ""
}

func parseCodexNotifyPayload(data []byte) (event, sessionID string) {
	var payload codexNotifyPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", ""
	}

	event = strings.TrimSpace(payload.HookEventName)
	if event == "" {
		event = strings.TrimSpace(payload.Type)
	}
	if event == "" {
		event = strings.TrimSpace(payload.Event)
	}
	if event == "" {
		event = strings.TrimSpace(payload.Method)
	}
	if event == "" {
		event = decodeStringField(payload.Params, "hook_event_name", "type", "event", "method")
	}
	if event == "" {
		event = decodeStringField(payload.Payload, "hook_event_name", "type", "event", "method")
	}

	sessionID = strings.TrimSpace(payload.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(payload.ThreadID)
	}
	if sessionID == "" {
		sessionID = strings.TrimSpace(payload.ThreadIDDash)
	}
	if sessionID == "" {
		sessionID = decodeStringField(payload.Params, "session_id", "thread_id", "thread-id", "id")
	}
	if sessionID == "" {
		sessionID = decodeStringField(payload.Payload, "session_id", "thread_id", "thread-id", "id")
	}

	return event, sessionID
}

// handleCodexNotify processes Codex notify payloads.
func handleCodexNotify() {
	instanceID := os.Getenv("AGENTDECK_INSTANCE_ID")
	if instanceID == "" {
		return
	}

	eventArg := ""
	var data []byte
	// Codex notify may pass payload in argv and/or stdin.
	if len(os.Args) > 2 {
		for _, rawArg := range os.Args[2:] {
			if len(rawArg) > maxHookPayloadSize {
				return
			}
			arg := rawArg
			arg = strings.TrimSpace(arg)
			if arg == "" {
				continue
			}
			if strings.HasPrefix(arg, "{") && strings.HasSuffix(arg, "}") {
				data = []byte(arg)
				break
			}
			if eventArg == "" {
				eventArg = arg
			}
		}
	}

	if len(data) == 0 {
		readData, err := io.ReadAll(io.LimitReader(os.Stdin, maxHookPayloadSize+1))
		if err != nil || len(readData) == 0 {
			readData = nil
		} else if len(readData) > maxHookPayloadSize {
			return
		}
		if len(readData) > 0 {
			data = readData
		}
	}

	event := ""
	sessionID := ""
	if len(data) > 0 {
		event, sessionID = parseCodexNotifyPayload(data)
		if event == "" {
			trimmed := strings.TrimSpace(string(data))
			if !strings.HasPrefix(trimmed, "{") {
				event = trimmed
			}
		}
	}
	if event == "" {
		event = eventArg
	}
	status := mapCodexNotifyToStatus(event)
	if status == "" {
		return
	}

	if sessionID == "" {
		sessionID = strings.TrimSpace(os.Getenv("CODEX_SESSION_ID"))
	}

	writeHookStatus(instanceID, status, sessionID, event, "")
}

func handleCodexHooks(args []string) {
	if len(args) == 0 {
		printCodexHooksUsage(os.Stderr)
		os.Exit(1)
	}

	switch args[0] {
	case "help", "--help", "-h":
		printCodexHooksUsage(os.Stdout)
	case "install":
		handleCodexHooksInstall()
	case "uninstall":
		handleCodexHooksUninstall()
	case "status":
		handleCodexHooksStatus()
	default:
		fmt.Fprintf(os.Stderr, "Unknown codex-hooks subcommand: %s\n", args[0])
		printCodexHooksUsage(os.Stderr)
		os.Exit(1)
	}
}

func printCodexHooksUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: agent-deck codex-hooks <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Manage Codex notify and lifecycle hook integration.")
	fmt.Fprintln(w, "Lifecycle status edges are advisory; notify and pane/process fallbacks remain active.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  install      Install or upgrade Codex notify and lifecycle hooks")
	fmt.Fprintln(w, "  uninstall    Remove agent-deck Codex notify and lifecycle hooks")
	fmt.Fprintln(w, "  status       Show current notify and lifecycle hook status")
}

func handleCodexHooksInstall() {
	configPath := getCodexConfigPath()
	hooksPath := getCodexHooksPath()
	notifyChanged, lifecycleChanged, err := installCodexHooks(configPath, hooksPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error installing Codex hooks: %v\n", err)
		os.Exit(1)
	}

	if notifyChanged {
		fmt.Println("Notify: INSTALLED")
	} else {
		fmt.Println("Notify: ALREADY INSTALLED")
	}
	if lifecycleChanged {
		fmt.Println("Lifecycle: INSTALLED")
	} else {
		fmt.Println("Lifecycle: ALREADY INSTALLED")
	}
	fmt.Printf("Config: %s\n", configPath)
	fmt.Printf("Hooks: %s\n", hooksPath)
	printCodexLifecycleTrustGuidance(os.Stdout)
}

func handleCodexHooksUninstall() {
	configPath := getCodexConfigPath()
	hooksPath := getCodexHooksPath()
	notifyChanged, lifecycleChanged, err := uninstallCodexHooks(configPath, hooksPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error uninstalling Codex hooks: %v\n", err)
		os.Exit(1)
	}

	if lifecycleChanged {
		fmt.Println("Lifecycle: REMOVED")
	} else {
		fmt.Println("Lifecycle: NOT INSTALLED")
	}
	if notifyChanged {
		fmt.Println("Notify: REMOVED")
	} else {
		fmt.Println("Notify: NOT INSTALLED OR CUSTOM")
	}
	fmt.Printf("Config: %s\n", configPath)
	fmt.Printf("Hooks: %s\n", hooksPath)
}

func handleCodexHooksStatus() {
	configPath := getCodexConfigPath()
	hooksPath := getCodexHooksPath()
	overall, notify, lifecycle, err := codexHooksStatus(configPath, hooksPath)

	fmt.Printf("Status: %s\n", overall)
	fmt.Printf("Notify: %s\n", notify)
	fmt.Printf("Lifecycle: %s\n", lifecycle)
	fmt.Printf("Config: %s\n", configPath)
	fmt.Printf("Hooks: %s\n", hooksPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Status error: %v\n", err)
	}
	if lifecycle == codexComponentInstalled || lifecycle == codexComponentPartial {
		printCodexLifecycleTrustGuidance(os.Stdout)
	}
}

func getCodexConfigPath() string {
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		return filepath.Join(codexHome, "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".codex", "config.toml")
	}
	return filepath.Join(home, ".codex", "config.toml")
}

func readFileOrEmpty(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func prependCodexNotifyBlock(block, content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return block
	}
	return strings.TrimRight(block, "\n") + "\n\n" + trimmed + "\n"
}

func removeLegacyCodexNotifyTable(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	removed := false

	for idx := 0; idx < len(lines); idx++ {
		line := lines[idx]
		if strings.TrimSpace(line) != "[notify]" {
			out = append(out, line)
			continue
		}

		progIdx := idx + 1
		for progIdx < len(lines) && strings.TrimSpace(lines[progIdx]) == "" {
			progIdx++
		}
		if progIdx < len(lines) && codexLegacyNotifyProgramLineRe.MatchString(lines[progIdx]) {
			removed = true
			idx = progIdx
			continue
		}

		out = append(out, line)
	}

	if !removed {
		return content, false
	}

	updated := strings.TrimSpace(strings.Join(out, "\n"))
	if updated != "" {
		updated += "\n"
	}
	return updated, true
}

func hasLegacyCodexNotifyTable(content string) bool {
	_, removed := removeLegacyCodexNotifyTable(content)
	return removed
}

func removeExactCodexNotifyLine(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	removed := false

	for _, line := range lines {
		if codexNotifyExactLineRe.MatchString(line) {
			removed = true
			continue
		}
		out = append(out, line)
	}

	if !removed {
		return content, false
	}

	updated := strings.TrimSpace(strings.Join(out, "\n"))
	if updated != "" {
		updated += "\n"
	}
	return updated, true
}

type codexComponentState string

const (
	codexComponentInstalled    codexComponentState = "INSTALLED"
	codexComponentLegacy       codexComponentState = "LEGACY"
	codexComponentCustom       codexComponentState = "CUSTOM"
	codexComponentPartial      codexComponentState = "PARTIAL"
	codexComponentNotInstalled codexComponentState = "NOT INSTALLED"
	codexComponentInvalid      codexComponentState = "INVALID"
	codexComponentError        codexComponentState = "ERROR"
)

type codexLifecycleDocument struct {
	top          map[string]json.RawMessage
	hooks        map[string]json.RawMessage
	hooksPresent bool
}

func getCodexHooksPath() string {
	return filepath.Join(filepath.Dir(getCodexConfigPath()), "hooks.json")
}

func codexNotifyBlock() string {
	return codexNotifyMarkerBegin + "\n" +
		codexNotifyLine + "\n" +
		codexNotifyMarkerEnd + "\n"
}

func codexNotifyMarkerBounds(content string) (begin, end int, found bool, err error) {
	begin = strings.Index(content, codexNotifyMarkerBegin)
	if begin == -1 {
		return 0, 0, false, nil
	}

	endRel := strings.Index(content[begin:], codexNotifyMarkerEnd)
	if endRel == -1 {
		return 0, 0, true, fmt.Errorf("malformed agent-deck Codex notify block")
	}
	return begin, begin + endRel + len(codexNotifyMarkerEnd), true, nil
}

func planCodexNotifyInstall(content string) (string, bool, error) {
	block := codexNotifyBlock()
	begin, end, found, err := codexNotifyMarkerBounds(content)
	if err != nil {
		return content, false, err
	}
	if found {
		outside := content[:begin] + content[end:]
		if codexNotifyKeyRe.MatchString(outside) || codexNotifyTableRe.MatchString(outside) {
			return content, false, fmt.Errorf("existing conflicting notify setting found")
		}
		if strings.TrimSpace(content[begin:end]) == strings.TrimSpace(block) {
			return content, false, nil
		}
		updated := prependCodexNotifyBlock(block, outside)
		return updated, updated != content, nil
	}

	if updated, removed := removeLegacyCodexNotifyTable(content); removed {
		updated = prependCodexNotifyBlock(block, updated)
		return updated, updated != content, nil
	}
	if codexNotifyExactRe.MatchString(content) {
		return content, false, nil
	}
	if codexNotifyKeyRe.MatchString(content) || codexNotifyTableRe.MatchString(content) {
		return content, false, fmt.Errorf("existing conflicting notify setting found")
	}

	updated := prependCodexNotifyBlock(block, content)
	return updated, updated != content, nil
}

func planCodexNotifyUninstall(content string) (string, bool, error) {
	begin, end, found, err := codexNotifyMarkerBounds(content)
	if err != nil {
		return content, false, err
	}
	if found {
		updated := strings.TrimSpace(content[:begin] + content[end:])
		if updated != "" {
			updated += "\n"
		}
		return updated, updated != content, nil
	}
	if updated, removed := removeLegacyCodexNotifyTable(content); removed {
		return updated, true, nil
	}
	if updated, removed := removeExactCodexNotifyLine(content); removed {
		return updated, true, nil
	}
	return content, false, nil
}

func codexNotifyStatus(content string) (codexComponentState, error) {
	_, _, found, err := codexNotifyMarkerBounds(content)
	if err != nil {
		return codexComponentError, err
	}
	if found || codexNotifyExactRe.MatchString(content) {
		return codexComponentInstalled, nil
	}
	if hasLegacyCodexNotifyTable(content) {
		return codexComponentLegacy, nil
	}
	if codexNotifyKeyRe.MatchString(content) || codexNotifyTableRe.MatchString(content) {
		return codexComponentCustom, nil
	}
	return codexComponentNotInstalled, nil
}

func readCodexHooksFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func decodeJSONObject(data []byte, label string) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("%s must be a JSON object", label)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", label, err)
	}
	if object == nil {
		return nil, fmt.Errorf("%s must be a JSON object", label)
	}
	return object, nil
}

func decodeJSONArray(data []byte, label string) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("%s must be a JSON array", label)
	}
	var array []json.RawMessage
	if err := json.Unmarshal(trimmed, &array); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", label, err)
	}
	if array == nil {
		return nil, fmt.Errorf("%s must be a JSON array", label)
	}
	return array, nil
}

func parseCodexLifecycleDocument(data []byte, exists bool) (*codexLifecycleDocument, error) {
	if !exists {
		return &codexLifecycleDocument{
			top:   make(map[string]json.RawMessage),
			hooks: make(map[string]json.RawMessage),
		}, nil
	}

	top, err := decodeJSONObject(data, "Codex hooks.json")
	if err != nil {
		return nil, err
	}
	doc := &codexLifecycleDocument{
		top:   top,
		hooks: make(map[string]json.RawMessage),
	}
	if rawHooks, ok := top["hooks"]; ok {
		hooks, err := decodeJSONObject(rawHooks, "hooks")
		if err != nil {
			return nil, err
		}
		doc.hooks = hooks
		doc.hooksPresent = true
	}

	for _, event := range codexLifecycleEvents {
		if rawEvent, ok := doc.hooks[event]; ok {
			if _, _, err := inspectCodexLifecycleEvent(rawEvent, event); err != nil {
				return nil, err
			}
		}
	}
	return doc, nil
}

func inspectCodexLifecycleEvent(rawEvent json.RawMessage, event string) ([]json.RawMessage, bool, error) {
	groups, err := decodeJSONArray(rawEvent, "hooks."+event)
	if err != nil {
		return nil, false, err
	}

	installed := false
	for index, rawGroup := range groups {
		group, err := decodeJSONObject(rawGroup, fmt.Sprintf("hooks.%s[%d]", event, index))
		if err != nil {
			return nil, false, err
		}
		rawHandlers, ok := group["hooks"]
		if !ok {
			continue
		}
		handlers, err := decodeJSONArray(rawHandlers, fmt.Sprintf("hooks.%s[%d].hooks", event, index))
		if err != nil {
			return nil, false, err
		}
		for handlerIndex, rawHandler := range handlers {
			handler, err := decodeJSONObject(rawHandler, fmt.Sprintf("hooks.%s[%d].hooks[%d]", event, index, handlerIndex))
			if err != nil {
				return nil, false, err
			}
			if isAgentDeckCodexLifecycleHandler(handler) {
				installed = true
			}
		}
	}
	return groups, installed, nil
}

func isAgentDeckCodexLifecycleHandler(handler map[string]json.RawMessage) bool {
	var handlerType, command string
	if err := json.Unmarshal(handler["type"], &handlerType); err != nil {
		return false
	}
	if err := json.Unmarshal(handler["command"], &command); err != nil {
		return false
	}
	return handlerType == "command" && strings.TrimSpace(command) == codexLifecycleCommand
}

func canonicalCodexLifecycleGroup() json.RawMessage {
	return json.RawMessage(`{"hooks":[{"type":"command","command":"agent-deck codex-notify","timeout":3}]}`)
}

func marshalCodexLifecycleDocument(doc *codexLifecycleDocument) ([]byte, error) {
	if doc.hooksPresent {
		rawHooks, err := json.Marshal(doc.hooks)
		if err != nil {
			return nil, err
		}
		doc.top["hooks"] = rawHooks
	}
	data, err := json.MarshalIndent(doc.top, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func codexLifecycleHandlerCount(doc *codexLifecycleDocument) (int, error) {
	count := 0
	for _, event := range codexLifecycleEvents {
		rawEvent, ok := doc.hooks[event]
		if !ok {
			continue
		}
		_, installed, err := inspectCodexLifecycleEvent(rawEvent, event)
		if err != nil {
			return 0, err
		}
		if installed {
			count++
		}
	}
	return count, nil
}

func planCodexLifecycleInstall(data []byte, exists bool) ([]byte, bool, error) {
	doc, err := parseCodexLifecycleDocument(data, exists)
	if err != nil {
		return data, false, err
	}

	changed := false
	for _, event := range codexLifecycleEvents {
		rawEvent, eventExists := doc.hooks[event]
		groups := []json.RawMessage(nil)
		installed := false
		if eventExists {
			groups, installed, err = inspectCodexLifecycleEvent(rawEvent, event)
			if err != nil {
				return data, false, err
			}
		}
		if installed {
			continue
		}
		groups = append(groups, canonicalCodexLifecycleGroup())
		encodedGroups, err := json.Marshal(groups)
		if err != nil {
			return data, false, err
		}
		doc.hooks[event] = encodedGroups
		doc.hooksPresent = true
		changed = true
	}
	if !exists {
		description, err := json.Marshal(codexLifecycleDescription)
		if err != nil {
			return data, false, err
		}
		doc.top["description"] = description
		changed = true
	}
	if !changed {
		return data, false, nil
	}

	updated, err := marshalCodexLifecycleDocument(doc)
	if err != nil {
		return data, false, err
	}
	return updated, true, nil
}

func removeAgentDeckCodexLifecycleHandlers(groups []json.RawMessage, event string) ([]json.RawMessage, bool, error) {
	updated := make([]json.RawMessage, 0, len(groups))
	removed := false

	for groupIndex, rawGroup := range groups {
		group, err := decodeJSONObject(rawGroup, fmt.Sprintf("hooks.%s[%d]", event, groupIndex))
		if err != nil {
			return nil, false, err
		}
		rawHandlers, hasHandlers := group["hooks"]
		if !hasHandlers {
			updated = append(updated, rawGroup)
			continue
		}
		handlers, err := decodeJSONArray(rawHandlers, fmt.Sprintf("hooks.%s[%d].hooks", event, groupIndex))
		if err != nil {
			return nil, false, err
		}
		kept := make([]json.RawMessage, 0, len(handlers))
		removedFromGroup := false
		for handlerIndex, rawHandler := range handlers {
			handler, err := decodeJSONObject(rawHandler, fmt.Sprintf("hooks.%s[%d].hooks[%d]", event, groupIndex, handlerIndex))
			if err != nil {
				return nil, false, err
			}
			if isAgentDeckCodexLifecycleHandler(handler) {
				removed = true
				removedFromGroup = true
				continue
			}
			kept = append(kept, rawHandler)
		}
		if !removedFromGroup {
			updated = append(updated, rawGroup)
			continue
		}
		if len(kept) == 0 && len(group) == 1 {
			continue
		}
		encodedHandlers, err := json.Marshal(kept)
		if err != nil {
			return nil, false, err
		}
		group["hooks"] = encodedHandlers
		encodedGroup, err := json.Marshal(group)
		if err != nil {
			return nil, false, err
		}
		updated = append(updated, encodedGroup)
	}
	return updated, removed, nil
}

func hasAgentDeckCodexLifecycleDescription(top map[string]json.RawMessage) bool {
	rawDescription, ok := top["description"]
	if !ok {
		return false
	}
	var description string
	return json.Unmarshal(rawDescription, &description) == nil && description == codexLifecycleDescription
}

func planCodexLifecycleUninstall(data []byte, exists bool) ([]byte, bool, error) {
	if !exists {
		return data, false, nil
	}
	doc, err := parseCodexLifecycleDocument(data, true)
	if err != nil {
		return data, false, err
	}

	changed := false
	for _, event := range codexLifecycleEvents {
		rawEvent, eventExists := doc.hooks[event]
		if !eventExists {
			continue
		}
		groups, _, err := inspectCodexLifecycleEvent(rawEvent, event)
		if err != nil {
			return data, false, err
		}
		updatedGroups, removed, err := removeAgentDeckCodexLifecycleHandlers(groups, event)
		if err != nil {
			return data, false, err
		}
		if !removed {
			continue
		}
		changed = true
		if len(updatedGroups) == 0 {
			delete(doc.hooks, event)
			continue
		}
		encodedGroups, err := json.Marshal(updatedGroups)
		if err != nil {
			return data, false, err
		}
		doc.hooks[event] = encodedGroups
	}

	remaining, err := codexLifecycleHandlerCount(doc)
	if err != nil {
		return data, false, err
	}
	if remaining == 0 && hasAgentDeckCodexLifecycleDescription(doc.top) {
		delete(doc.top, "description")
		changed = true
	}
	if !changed {
		return data, false, nil
	}

	updated, err := marshalCodexLifecycleDocument(doc)
	if err != nil {
		return data, false, err
	}
	return updated, true, nil
}

func codexLifecycleStatus(data []byte, exists bool) (codexComponentState, error) {
	if !exists {
		return codexComponentNotInstalled, nil
	}
	doc, err := parseCodexLifecycleDocument(data, true)
	if err != nil {
		return codexComponentInvalid, err
	}
	count, err := codexLifecycleHandlerCount(doc)
	if err != nil {
		return codexComponentInvalid, err
	}
	switch count {
	case 0:
		return codexComponentNotInstalled, nil
	case len(codexLifecycleEvents):
		return codexComponentInstalled, nil
	default:
		return codexComponentPartial, nil
	}
}

func installCodexHooks(configPath, hooksPath string) (notifyChanged, lifecycleChanged bool, err error) {
	configContent, err := readFileOrEmpty(configPath)
	if err != nil {
		return false, false, fmt.Errorf("read config: %w", err)
	}
	hooksContent, hooksExists, err := readCodexHooksFile(hooksPath)
	if err != nil {
		return false, false, fmt.Errorf("read hooks: %w", err)
	}

	updatedConfig, notifyChanged, err := planCodexNotifyInstall(configContent)
	if err != nil {
		return false, false, err
	}
	updatedHooks, lifecycleChanged, err := planCodexLifecycleInstall(hooksContent, hooksExists)
	if err != nil {
		return false, false, err
	}

	if notifyChanged {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return false, false, fmt.Errorf("create config directory: %w", err)
		}
	}
	if lifecycleChanged {
		if err := os.MkdirAll(filepath.Dir(hooksPath), 0755); err != nil {
			return false, false, fmt.Errorf("create hooks directory: %w", err)
		}
	}
	if notifyChanged {
		if err := os.WriteFile(configPath, []byte(updatedConfig), 0644); err != nil {
			return false, false, fmt.Errorf("write config: %w", err)
		}
	}
	if lifecycleChanged {
		if err := os.WriteFile(hooksPath, updatedHooks, 0644); err != nil {
			return false, false, fmt.Errorf("write hooks: %w", err)
		}
	}
	return notifyChanged, lifecycleChanged, nil
}

func uninstallCodexHooks(configPath, hooksPath string) (notifyChanged, lifecycleChanged bool, err error) {
	configContent, err := readFileOrEmpty(configPath)
	if err != nil {
		return false, false, fmt.Errorf("read config: %w", err)
	}
	hooksContent, hooksExists, err := readCodexHooksFile(hooksPath)
	if err != nil {
		return false, false, fmt.Errorf("read hooks: %w", err)
	}

	updatedConfig, notifyChanged, err := planCodexNotifyUninstall(configContent)
	if err != nil {
		return false, false, err
	}
	updatedHooks, lifecycleChanged, err := planCodexLifecycleUninstall(hooksContent, hooksExists)
	if err != nil {
		return false, false, err
	}

	if lifecycleChanged {
		if err := os.MkdirAll(filepath.Dir(hooksPath), 0755); err != nil {
			return false, false, fmt.Errorf("create hooks directory: %w", err)
		}
		if err := os.WriteFile(hooksPath, updatedHooks, 0644); err != nil {
			return false, false, fmt.Errorf("write hooks: %w", err)
		}
	}
	if notifyChanged {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return false, false, fmt.Errorf("create config directory: %w", err)
		}
		if err := os.WriteFile(configPath, []byte(updatedConfig), 0644); err != nil {
			return false, false, fmt.Errorf("write config: %w", err)
		}
	}
	return notifyChanged, lifecycleChanged, nil
}

func codexHooksStatus(configPath, hooksPath string) (string, codexComponentState, codexComponentState, error) {
	configContent, configErr := readFileOrEmpty(configPath)
	notify := codexComponentError
	var statusErr error
	if configErr != nil {
		statusErr = fmt.Errorf("read config: %w", configErr)
	} else {
		var err error
		notify, err = codexNotifyStatus(configContent)
		if err != nil {
			statusErr = err
		}
	}

	hooksContent, hooksExists, hooksErr := readCodexHooksFile(hooksPath)
	lifecycle := codexComponentError
	if hooksErr != nil {
		if statusErr == nil {
			statusErr = fmt.Errorf("read hooks: %w", hooksErr)
		}
	} else {
		var err error
		lifecycle, err = codexLifecycleStatus(hooksContent, hooksExists)
		if err != nil && statusErr == nil {
			statusErr = err
		}
	}

	if statusErr != nil || notify == codexComponentError || lifecycle == codexComponentInvalid || lifecycle == codexComponentError {
		return "ERROR", notify, lifecycle, statusErr
	}
	if notify == codexComponentInstalled && lifecycle == codexComponentInstalled {
		return "INSTALLED", notify, lifecycle, nil
	}
	if notify == codexComponentNotInstalled && lifecycle == codexComponentNotInstalled {
		return "NOT INSTALLED", notify, lifecycle, nil
	}
	return "PARTIAL", notify, lifecycle, nil
}

func printCodexLifecycleTrustGuidance(w io.Writer) {
	fmt.Fprintln(w, "Open /hooks in Codex and trust the Agent Deck UserPromptSubmit and Stop hooks.")
	fmt.Fprintln(w, "Until trusted and enabled, Agent Deck continues using notify and pane/process fallback.")
}
