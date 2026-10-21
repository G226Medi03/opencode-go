package opencode

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// maxSearchDepth bounds the tolerant search through unknown event payloads.
const maxSearchDepth = 12

// Event is one JSON line emitted by `opencode run --format json`.
//
// The schema is documented in docs/opencode-json.md; raven still keeps the raw
// payload so unknown event kinds never break a run.
type Event struct {
	// Type is the event kind, for example step_start, tool_use or text.
	Type string
	// Tool is set for tool_use events (part.tool).
	Tool string
	// Raw is the original JSON line.
	Raw json.RawMessage
	// JSON is the decoded payload.
	JSON map[string]any
}

// ToolCall is one tool invocation reported by OpenCode.
//
// Confirmed shape (v2.0.32):
//
//	{"type":"tool_use","sessionID":"ses_...","part":{"type":"tool","id":"call_...",
//	  "tool":"read","state":{"status":"completed","input":{"path":"..."},"output":"..."}}}
type ToolCall struct {
	ID     string
	Tool   string
	Status string
	Input  map[string]any
	Output string
}

// CountSteps returns the number of model steps in the event stream.
func CountSteps(events []Event) int {
	steps := 0
	for _, ev := range events {
		if ev.Type == "step_start" {
			steps++
		}
	}
	return steps
}

// ExtractToolCalls returns the tool invocations found in the events.
func ExtractToolCalls(events []Event) []ToolCall {
	var calls []ToolCall

	for _, ev := range events {
		if ev.Type != "tool_use" {
			continue
		}
		part, ok := ev.JSON["part"].(map[string]any)
		if !ok {
			continue
		}

		call := ToolCall{}
		call.Tool, _ = part["tool"].(string)
		call.ID, _ = part["id"].(string)
		if state, ok := part["state"].(map[string]any); ok {
			call.Status, _ = state["status"].(string)
			if input, ok := state["input"].(map[string]any); ok {
				call.Input = input
			}
			call.Output, _ = state["output"].(string)
		}
		calls = append(calls, call)
	}
	return calls
}

// ParseEvent parses a single JSON line. It returns ok=false for blank lines and
// for lines that are not JSON objects, so stray diagnostics never abort a run.
func ParseEvent(line []byte) (Event, bool, error) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" || trimmed[0] != '{' {
		return Event{}, false, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return Event{}, false, err
	}
	return Event{
		Type: firstString(payload, "type", "event", "kind", "name"),
		Tool: toolName(payload),
		Raw:  json.RawMessage(trimmed),
		JSON: payload,
	}, true, nil
}

// toolName extracts part.tool from a tool_use event.
func toolName(payload map[string]any) string {
	part, ok := payload["part"].(map[string]any)
	if !ok {
		return ""
	}
	name, _ := part["tool"].(string)
	return name
}

// ParseStream reads newline separated JSON events from r.
func ParseStream(r io.Reader, handle func(Event)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		ev, ok, err := ParseEvent(scanner.Bytes())
		if err != nil {
			// Ignore malformed lines; they are usually human readable logs.
			continue
		}
		if ok && handle != nil {
			handle(ev)
		}
	}
	return scanner.Err()
}

// Usage is token accounting extracted from an event stream.
//
// Confirmed shape (OpenCode v2.0.32):
//
//	{"type":"step_finish", ...,"part":{"type":"step-finish","reason":"stop",
//	  "cost":0.00096,"tokens":{"input":6408,"output":2,"reasoning":0,
//	  "cache":{"read":256,"write":0}}}}
type Usage struct {
	Input      int64
	Output     int64
	Reasoning  int64
	CacheRead  int64
	CacheWrite int64
	Cost       float64
}

// ExtractUsage sums usage blocks found in the events.
func ExtractUsage(events []Event) Usage {
	var usage Usage
	for _, ev := range events {
		collectUsage(ev.JSON, 0, &usage)
	}
	return usage
}

// ExtractSessionID returns the first session identifier found in the events.
func ExtractSessionID(events []Event) string {
	for _, ev := range events {
		if id, ok := findString(ev.JSON, 0, "sessionID", "session_id", "sessionId"); ok {
			return id
		}
	}
	return ""
}

// ExtractFinalText returns the last assistant text part in the stream.
// OpenCode emits these as events with type "text" (payload at part.text).
func ExtractFinalText(events []Event) string {
	var text string
	for _, ev := range events {
		if ev.Type != "text" {
			continue
		}
		if s, ok := findString(ev.JSON, 0, "text"); ok {
			text = s
		}
	}
	return text
}

// ExtractError returns the first error message reported in the stream.
//
// Confirmed shape:
//
//	{"type":"error","error":{"type":"unknown","message":"Agent not found: ..."}}
func ExtractError(events []Event) string {
	for _, ev := range events {
		if ev.Type == "error" {
			if msg, ok := findString(ev.JSON, 0, "message", "error"); ok {
				return msg
			}
			return "opencode reported an error"
		}
	}
	return ""
}

func firstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := payload[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func findString(value any, depth int, keys ...string) (string, bool) {
	if depth > maxSearchDepth {
		return "", false
	}
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if v, ok := typed[key].(string); ok && v != "" {
				return v, true
			}
		}
		for _, child := range typed {
			if found, ok := findString(child, depth+1, keys...); ok {
				return found, true
			}
		}
	case []any:
		for _, child := range typed {
			if found, ok := findString(child, depth+1, keys...); ok {
				return found, true
			}
		}
	}
	return "", false
}

func collectUsage(value any, depth int, usage *Usage) {
	if depth > maxSearchDepth {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		// OpenCode v2 reports tokens under part.tokens with a nested cache object:
		// {"input":..,"output":..,"reasoning":..,"cache":{"read":..,"write":..}}
		if _, hasInput := typed["input"]; hasInput {
			if _, hasOutput := typed["output"]; hasOutput {
				usage.Input += toInt64(typed["input"])
				usage.Output += toInt64(typed["output"])
				usage.Reasoning += toInt64(typed["reasoning"])
				if cache, ok := typed["cache"].(map[string]any); ok {
					usage.CacheRead += toInt64(cache["read"])
					usage.CacheWrite += toInt64(cache["write"])
				}
				usage.CacheRead += toInt64(typed["cacheRead"]) + toInt64(typed["cache_read"])
				usage.CacheWrite += toInt64(typed["cacheWrite"]) + toInt64(typed["cache_write"])
				return
			}
		}
		// Cost is reported next to tokens, for example part.cost.
		if cost, ok := typed["cost"]; ok {
			usage.Cost += toFloat64(cost)
		}
		for _, child := range typed {
			collectUsage(child, depth+1, usage)
		}
	case []any:
		for _, child := range typed {
			collectUsage(child, depth+1, usage)
		}
	}
}

func toInt64(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	}
	return 0
}

func toFloat64(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int64:
		return float64(typed)
	case int:
		return float64(typed)
	}
	return 0
}
