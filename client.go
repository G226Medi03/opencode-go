package opencode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/G226Medi03/opencode-go/internal/proc"
)

// AgentRequest describes a single `opencode run` invocation.
type AgentRequest struct {
	// Bin overrides the opencode binary for this invocation.
	Bin string
	// Agent is the OpenCode agent id, such as "raven-implementer".
	Agent string
	// Model is "provider/model" with an optional "#variant".
	Model string
	// Session continues (or creates) a specific session.
	Session string
	// Fork forks the session before continuing.
	Fork bool
	// Prompt is the user message.
	Prompt string
	// Files are attached files passed with --file.
	Files []string
	// Dir is the working directory for the child process.
	Dir string
	// Env adds environment variables on top of the current environment.
	Env []string
	// Auto passes --auto so non-denied permissions are approved.
	Auto bool
	// Standalone runs with a private server instead of the shared service.
	Standalone bool
	// Timeout bounds the whole invocation. Zero means no timeout.
	Timeout time.Duration
	// IdleTimeout aborts the invocation when no event arrives for this long.
	IdleTimeout time.Duration
	// OnEvent receives parsed events as they arrive.
	OnEvent func(Event)
	// EventWriter receives the raw JSON lines when set.
	EventWriter io.Writer
}

// AgentResult is the outcome of one invocation.
type AgentResult struct {
	SessionID string
	ExitCode  int
	Events    []Event
	Tools     []ToolCall
	Steps     int
	Usage     Usage
	Stderr    string
	Duration  time.Duration
}

// Runner executes one agent invocation.
type Runner interface {
	Run(ctx context.Context, req AgentRequest) (AgentResult, error)
}

// Client runs the opencode CLI as a subprocess.
type Client struct {
	Bin string
}

// Args builds the argument vector for `opencode run`. Exported for tests.
func Args(req AgentRequest) []string {
	args := []string{"run", "--format", "json"}
	if req.Agent != "" {
		args = append(args, "--agent", req.Agent)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Session != "" {
		args = append(args, "--session", req.Session)
	}
	if req.Fork {
		args = append(args, "--fork")
	}
	if req.Auto {
		args = append(args, "--auto")
	}
	if req.Standalone {
		args = append(args, "--standalone")
	}
	for _, file := range req.Files {
		args = append(args, "--file", file)
	}
	if strings.TrimSpace(req.Prompt) != "" {
		args = append(args, req.Prompt)
	}
	return args
}

// childEnv returns the parent environment without raven's own secrets. Provider
// credentials are left intact because OpenCode needs them; raven's GitHub and
// notification secrets must not be visible to agent shells.
func childEnv(extra []string) []string {
	base := os.Environ()
	env := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		key := strings.SplitN(kv, "=", 2)[0]
		if strings.HasPrefix(key, "RAVEN_") {
			continue
		}
		env = append(env, kv)
	}
	for _, kv := range extra {
		key := strings.SplitN(kv, "=", 2)[0]
		if strings.HasPrefix(key, "RAVEN_") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// Run executes one opencode agent invocation.
//
// A non-zero child exit code is reported through AgentResult.ExitCode together
// with a non-nil error, so callers must inspect the result even on error.
func (c *Client) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	result := AgentResult{}

	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	bin := req.Bin
	if bin == "" {
		bin = c.Bin
	}
	path, err := ResolveBin(bin)
	if err != nil {
		return result, err
	}

	cmd := exec.CommandContext(runCtx, path, Args(req)...)
	if req.Dir != "" {
		cmd.Dir = req.Dir
	}
	cmd.Env = childEnv(req.Env)
	proc.Configure(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return result, fmt.Errorf("open stdout: %w", err)
	}
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("start opencode: %w", err)
	}

	// Terminate the whole tree if the context is cancelled.
	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-runCtx.Done():
			proc.KillTree(cmd)
		case <-stopWatch:
		}
	}()
	defer close(stopWatch)

	// Liveness watchdog: abort when the agent stops producing output.
	var idle *time.Timer
	idleFired := false
	if req.IdleTimeout > 0 {
		idle = time.AfterFunc(req.IdleTimeout, func() {
			idleFired = true
			cancelRun()
		})
		defer idle.Stop()
	}

	parseErr := ParseStream(stdout, func(ev Event) {
		result.Events = append(result.Events, ev)
		if idle != nil {
			idle.Reset(req.IdleTimeout)
		}
		if req.EventWriter != nil {
			if _, err := req.EventWriter.Write(append(append([]byte{}, ev.Raw...), '\n')); err != nil {
				return
			}
		}
		if req.OnEvent != nil {
			req.OnEvent(ev)
		}
	})

	waitErr := cmd.Wait()
	result.Duration = time.Since(started)
	result.Stderr = stderrBuf.String()
	result.Usage = ExtractUsage(result.Events)
	result.SessionID = ExtractSessionID(result.Events)
	result.Tools = ExtractToolCalls(result.Events)
	result.Steps = CountSteps(result.Events)

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	if idleFired {
		return result, fmt.Errorf("opencode produced no output for %s", req.IdleTimeout)
	}
	if ctx.Err() != nil {
		return result, fmt.Errorf("opencode run aborted: %w", ctx.Err())
	}
	if parseErr != nil {
		return result, fmt.Errorf("read opencode events: %w", parseErr)
	}
	// OpenCode can report failures as error events while still exiting 0.
	if message := ExtractError(result.Events); message != "" {
		return result, fmt.Errorf("opencode error: %s", message)
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			return result, fmt.Errorf("wait opencode: %w", waitErr)
		}
	}
	if result.ExitCode != 0 {
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = "no stderr output"
		}
		return result, fmt.Errorf("opencode exited with code %d: %s", result.ExitCode, message)
	}
	return result, nil
}
