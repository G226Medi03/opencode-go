package opencode

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseStreamAcceptsTenMegabyteLine(t *testing.T) {
	payload := strings.Repeat("x", 10*1024*1024)
	line := `{"type":"text","part":{"text":"` + payload + `"}}` + "\n"

	var got Event
	require.NoError(t, ParseStream(strings.NewReader(line), func(ev Event) { got = ev }))
	require.Equal(t, "text", got.Type)
}

func TestParseEventSkipsNonJSON(t *testing.T) {
	_, ok, err := ParseEvent([]byte("plain log line"))
	require.NoError(t, err)
	require.False(t, ok)

	_, ok, err = ParseEvent([]byte("   "))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestParseStreamSkipsMalformedLines(t *testing.T) {
	input := "not json\n{\"type\":\"a\"}\n{\"broken\":\n{\"type\":\"b\"}\n"

	var got []string
	require.NoError(t, ParseStream(strings.NewReader(input), func(ev Event) {
		got = append(got, ev.Type)
	}))
	require.Equal(t, []string{"a", "b"}, got)
}

// TestGoldenRealEventStream asserts against a captured OpenCode v2.0.32 stream.
func TestGoldenRealEventStream(t *testing.T) {
	f, err := os.Open("testdata/events.jsonl")
	require.NoError(t, err)
	defer f.Close()

	var events []Event
	require.NoError(t, ParseStream(f, func(ev Event) {
		events = append(events, ev)
	}))
	require.Len(t, events, 3)
	require.Equal(t, "step_start", events[0].Type)
	require.Equal(t, "text", events[1].Type)
	require.Equal(t, "step_finish", events[2].Type)

	require.Equal(t, "ses_eb233a838ffeloz41yc4vN3vVA", ExtractSessionID(events))
	require.Equal(t, "ok", ExtractFinalText(events))

	u := ExtractUsage(events)
	require.EqualValues(t, 6408, u.Input)
	require.EqualValues(t, 2, u.Output)
	require.EqualValues(t, 0, u.Reasoning)
	require.EqualValues(t, 256, u.CacheRead)
	require.EqualValues(t, 0, u.CacheWrite)
	require.InDelta(t, 0.000963168, u.Cost, 1e-9)
}

func TestExtractUsageSumsMultipleSteps(t *testing.T) {
	events := []Event{
		{JSON: map[string]any{"part": map[string]any{
			"cost":   0.5,
			"tokens": map[string]any{"input": float64(100), "output": float64(20), "cache": map[string]any{"read": float64(40)}},
		}}},
		{JSON: map[string]any{"part": map[string]any{
			"cost":   0.25,
			"tokens": map[string]any{"input": float64(10), "output": float64(5)},
		}}},
	}

	u := ExtractUsage(events)
	require.EqualValues(t, 110, u.Input)
	require.EqualValues(t, 25, u.Output)
	require.EqualValues(t, 40, u.CacheRead)
	require.InDelta(t, 0.75, u.Cost, 1e-9)
}

func TestExtractSessionIDReturnsEmptyWhenAbsent(t *testing.T) {
	require.Empty(t, ExtractSessionID([]Event{{JSON: map[string]any{"type": "x"}}}))
}

func TestExtractErrorReadsNestedMessage(t *testing.T) {
	raw := `{"type":"error","timestamp":1,"sessionID":"ses_1","error":{"type":"unknown","message":"Agent not found: \"raven-nope\""}}`
	ev, ok, err := ParseEvent([]byte(raw))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "error", ev.Type)
	require.Equal(t, `Agent not found: "raven-nope"`, ExtractError([]Event{ev}))
}

func TestExtractErrorEmptyForNormalStream(t *testing.T) {
	require.Empty(t, ExtractError([]Event{{Type: "text", JSON: map[string]any{"type": "text"}}}))
}

func TestExtractToolCalls(t *testing.T) {
	raw := `{"type":"tool_use","timestamp":1,"sessionID":"ses_1","part":{"type":"tool","id":"call_1","tool":"read","state":{"status":"completed","input":{"path":"README.md","limit":30},"output":"Read file"}}}`
	ev, ok, err := ParseEvent([]byte(raw))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "tool_use", ev.Type)
	require.Equal(t, "read", ev.Tool)

	calls := ExtractToolCalls([]Event{ev})
	require.Len(t, calls, 1)
	require.Equal(t, "read", calls[0].Tool)
	require.Equal(t, "call_1", calls[0].ID)
	require.Equal(t, "completed", calls[0].Status)
	require.Equal(t, "README.md", calls[0].Input["path"])
	require.Equal(t, "Read file", calls[0].Output)
}

func TestExtractToolCallsIgnoresOtherEvents(t *testing.T) {
	events := []Event{
		{Type: "step_start", JSON: map[string]any{"part": map[string]any{"type": "step-start"}}},
		{Type: "text", JSON: map[string]any{"part": map[string]any{"text": "hi"}}},
	}
	require.Empty(t, ExtractToolCalls(events))
}
