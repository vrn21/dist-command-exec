package executor

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCommandExecutor_Name(t *testing.T) {
	e := NewCommandExecutor()
	if got := e.Name(); got != "command" {
		t.Errorf("Name() = %q, want %q", got, "command")
	}
}

func TestCommandExecutor_Validate(t *testing.T) {
	e := NewCommandExecutor()

	tests := []struct {
		name    string
		payload CommandPayload
		wantErr bool
	}{
		{
			name:    "valid simple command",
			payload: CommandPayload{Command: "echo", Args: []string{"hello"}},
			wantErr: false,
		},
		{
			name:    "empty command",
			payload: CommandPayload{Command: ""},
			wantErr: true,
		},
		{
			name: "command with all fields",
			payload: CommandPayload{
				Command:    "cat",
				Args:       []string{"-n"},
				Env:        map[string]string{"KEY": "value"},
				WorkingDir: "/tmp",
				Stdin:      "test input",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payloadBytes, _ := json.Marshal(tt.payload)
			err := e.Validate(payloadBytes)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCommandExecutor_Validate_InvalidJSON(t *testing.T) {
	e := NewCommandExecutor()
	err := e.Validate([]byte("not json"))
	if err == nil {
		t.Error("Validate() expected error for invalid JSON")
	}
}

func TestCommandExecutor_Validate_BlockedCommands(t *testing.T) {
	e := NewCommandExecutorWithLimits(nil, []string{"rm", "sudo"})

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{"allowed command", "ls", false},
		{"blocked rm", "rm", true},
		{"blocked sudo", "sudo", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, _ := json.Marshal(CommandPayload{Command: tt.command})
			err := e.Validate(payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCommandExecutor_Validate_AllowedCommands(t *testing.T) {
	e := NewCommandExecutorWithLimits([]string{"ls", "echo"}, nil)

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{"allowed ls", "ls", false},
		{"allowed echo", "echo", false},
		{"not allowed cat", "cat", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, _ := json.Marshal(CommandPayload{Command: tt.command})
			err := e.Validate(payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCommandExecutor_Execute(t *testing.T) {
	e := NewCommandExecutor()
	ctx := context.Background()

	tests := []struct {
		name         string
		payload      CommandPayload
		wantExitCode int
		wantOutput   string
		wantError    bool
	}{
		{
			name:         "simple echo",
			payload:      CommandPayload{Command: "echo", Args: []string{"hello"}},
			wantExitCode: 0,
			wantOutput:   "hello\n",
		},
		{
			name:         "exit with code",
			payload:      CommandPayload{Command: "sh", Args: []string{"-c", "exit 42"}},
			wantExitCode: 42,
		},
		{
			name:         "nonexistent command",
			payload:      CommandPayload{Command: "nonexistent_command_xyz123"},
			wantExitCode: -1,
			wantError:    true,
		},
		{
			name:         "echo with stdin",
			payload:      CommandPayload{Command: "cat", Stdin: "from stdin"},
			wantExitCode: 0,
			wantOutput:   "from stdin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payloadBytes, _ := json.Marshal(tt.payload)
			task := Task{ID: "test-" + tt.name, Type: "command", Payload: payloadBytes}

			result, err := e.Execute(ctx, task)
			if err != nil {
				t.Fatalf("Execute() returned error: %v", err)
			}

			if result.ExitCode != tt.wantExitCode {
				t.Errorf("ExitCode = %d, want %d", result.ExitCode, tt.wantExitCode)
			}

			if tt.wantOutput != "" && string(result.Output) != tt.wantOutput {
				t.Errorf("Output = %q, want %q", result.Output, tt.wantOutput)
			}

			if tt.wantError && result.Error == "" {
				t.Error("Expected error message in result")
			}
		})
	}
}

func TestCommandExecutor_Execute_Timeout(t *testing.T) {
	e := NewCommandExecutor()

	// Create a context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	payload, _ := json.Marshal(CommandPayload{Command: "sleep", Args: []string{"10"}})
	task := Task{ID: "timeout-test", Type: "command", Payload: payload}

	result, err := e.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	// Should have non-zero exit code due to timeout
	if result.ExitCode == 0 {
		t.Error("Expected non-zero exit code for timed out command")
	}

	if result.Error == "" {
		t.Error("Expected error message for timed out command")
	}
}

func TestCommandExecutor_Execute_Environment(t *testing.T) {
	e := NewCommandExecutor()
	ctx := context.Background()

	payload, _ := json.Marshal(CommandPayload{
		Command: "sh",
		Args:    []string{"-c", "echo $TEST_VAR"},
		Env:     map[string]string{"TEST_VAR": "test_value"},
	})
	task := Task{ID: "env-test", Type: "command", Payload: payload}

	result, err := e.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	if string(result.Output) != "test_value\n" {
		t.Errorf("Output = %q, want %q", result.Output, "test_value\n")
	}
}

func TestCommandExecutor_Execute_Stderr(t *testing.T) {
	e := NewCommandExecutor()
	ctx := context.Background()

	payload, _ := json.Marshal(CommandPayload{
		Command: "sh",
		Args:    []string{"-c", "echo 'stderr output' >&2"},
	})
	task := Task{ID: "stderr-test", Type: "command", Payload: payload}

	result, err := e.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	stderr, ok := result.Metadata["stderr"]
	if !ok {
		t.Fatal("stderr not in metadata")
	}
	if stderr != "stderr output\n" {
		t.Errorf("stderr = %q, want %q", stderr, "stderr output\n")
	}
}

func TestCommandExecutor_HealthCheck(t *testing.T) {
	e := NewCommandExecutor()
	ctx := context.Background()

	err := e.HealthCheck(ctx)
	if err != nil {
		t.Errorf("HealthCheck() error = %v, want nil", err)
	}
}
