package opencode

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChildEnvDropsRavenSecrets(t *testing.T) {
	t.Setenv("RAVEN_GITHUB_TOKEN", "secret")
	t.Setenv("KEEP_ME", "yes")

	env := childEnv([]string{"EXTRA=1", "RAVEN_EXTRA=no"})
	joined := strings.Join(env, "\n")
	require.NotContains(t, joined, "RAVEN_GITHUB_TOKEN")
	require.NotContains(t, joined, "RAVEN_EXTRA")
	require.Contains(t, joined, "KEEP_ME=yes")
	require.Contains(t, joined, "EXTRA=1")
}

func TestArgsFull(t *testing.T) {
	args := Args(AgentRequest{
		Agent:   "raven-implementer",
		Model:   "vendor/model#high",
		Session: "ses_1",
		Fork:    true,
		Auto:    true,
		Files:   []string{"a.go"},
		Prompt:  "do work",
	})

	require.Equal(t, []string{
		"run", "--format", "json",
		"--agent", "raven-implementer",
		"--model", "vendor/model#high",
		"--session", "ses_1",
		"--fork",
		"--auto",
		"--file", "a.go",
		"do work",
	}, args)
}

func TestArgsOmitsEmptyPrompt(t *testing.T) {
	args := Args(AgentRequest{Prompt: "   "})
	require.Equal(t, []string{"run", "--format", "json"}, args)
}

func TestArgsStandalone(t *testing.T) {
	args := Args(AgentRequest{Standalone: true})
	require.Equal(t, []string{"run", "--format", "json", "--standalone"}, args)
}
