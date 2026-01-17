package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CommandPayload is the input format for command execution.
type CommandPayload struct {
	// Command is the executable to run.
	Command string `json:"command"`

	// Args are the command arguments.
	Args []string `json:"args,omitempty"`

	// Env is additional environment variables.
	Env map[string]string `json:"env,omitempty"`

	// WorkingDir is the working directory for execution.
	WorkingDir string `json:"working_dir,omitempty"`

	// Stdin is optional input data.
	Stdin string `json:"stdin,omitempty"`
}

// CommandExecutor executes shell commands.
type CommandExecutor struct {
	// AllowedCommands is an optional whitelist of allowed commands.
	AllowedCommands []string

	// BlockedCommands is an optional blacklist of blocked commands.
	BlockedCommands []string
}

// NewCommandExecutor creates a new command executor.
func NewCommandExecutor() *CommandExecutor {
	return &CommandExecutor{}
}

// NewCommandExecutorWithLimits creates a command executor with security limits.
func NewCommandExecutorWithLimits(allowed, blocked []string) *CommandExecutor {
	return &CommandExecutor{
		AllowedCommands: allowed,
		BlockedCommands: blocked,
	}
}

// Name returns the executor type name.
func (e *CommandExecutor) Name() string {
	return "command"
}

// Validate checks if a task payload is valid.
func (e *CommandExecutor) Validate(payload []byte) error {
	var p CommandPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}

	if p.Command == "" {
		return fmt.Errorf("command is required")
	}

	// Check blocked commands
	if len(e.BlockedCommands) > 0 {
		for _, blocked := range e.BlockedCommands {
			if p.Command == blocked {
				return fmt.Errorf("command %q is blocked", p.Command)
			}
		}
	}

	// Check allowed commands
	if len(e.AllowedCommands) > 0 {
		allowed := false
		for _, a := range e.AllowedCommands {
			if p.Command == a {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("command %q is not in allowed list", p.Command)
		}
	}

	return nil
}

// Execute runs the command and returns the result.
func (e *CommandExecutor) Execute(ctx context.Context, task Task) (Result, error) {
	var payload CommandPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return Result{}, fmt.Errorf("payload parse error: %w", err)
	}

	// Build the command
	cmd := exec.CommandContext(ctx, payload.Command, payload.Args...)

	// Set working directory
	if payload.WorkingDir != "" {
		cmd.Dir = payload.WorkingDir
	}

	// Set environment
	if len(payload.Env) > 0 {
		env := os.Environ()
		for k, v := range payload.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}

	// Set stdin
	if payload.Stdin != "" {
		cmd.Stdin = strings.NewReader(payload.Stdin)
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute
	err := cmd.Run()

	// Build result
	result := Result{
		Output: stdout.Bytes(),
		Metadata: map[string]string{
			"stderr": stderr.String(),
		},
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			result.Error = fmt.Sprintf("command exited with code %d", result.ExitCode)
		} else if ctx.Err() != nil {
			// Context cancelled or deadline exceeded
			result.ExitCode = -1
			result.Error = ctx.Err().Error()
		} else {
			result.Error = err.Error()
			result.ExitCode = -1
		}
	}

	return result, nil
}

// HealthCheck verifies the executor can operate.
func (e *CommandExecutor) HealthCheck(ctx context.Context) error {
	// Simple check: can we run 'true' command?
	cmd := exec.CommandContext(ctx, "true")
	return cmd.Run()
}
