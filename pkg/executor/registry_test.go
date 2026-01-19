package executor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// mockExecutor is a test executor for registry tests.
type mockExecutor struct {
	name        string
	validateErr error
	executeErr  error
	healthErr   error
	result      Result
}

func (m *mockExecutor) Name() string { return m.name }

func (m *mockExecutor) Validate(payload []byte) error { return m.validateErr }

func (m *mockExecutor) Execute(ctx context.Context, task Task) (Result, error) {
	if m.executeErr != nil {
		return Result{}, m.executeErr
	}
	return m.result, nil
}

func (m *mockExecutor) HealthCheck(ctx context.Context) error { return m.healthErr }

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()

	// Before registration
	if _, ok := r.Get("test"); ok {
		t.Error("Get() found executor before registration")
	}

	// Register
	mock := &mockExecutor{name: "test"}
	r.Register(mock)

	// After registration
	e, ok := r.Get("test")
	if !ok {
		t.Fatal("Get() didn't find registered executor")
	}
	if e.Name() != "test" {
		t.Errorf("Got executor with name %q, want %q", e.Name(), "test")
	}
}

func TestRegistry_RegisterOverwrite(t *testing.T) {
	r := NewRegistry()

	mock1 := &mockExecutor{name: "test", result: Result{Output: []byte("first")}}
	mock2 := &mockExecutor{name: "test", result: Result{Output: []byte("second")}}

	r.Register(mock1)
	r.Register(mock2)

	e, _ := r.Get("test")
	result, _ := e.Execute(context.Background(), Task{})

	if string(result.Output) != "second" {
		t.Error("Registration should overwrite existing executor")
	}
}

func TestRegistry_Capabilities(t *testing.T) {
	r := NewRegistry()

	caps := r.Capabilities()
	if len(caps) != 0 {
		t.Errorf("Capabilities() = %v, want empty", caps)
	}

	r.Register(&mockExecutor{name: "exec1"})
	r.Register(&mockExecutor{name: "exec2"})

	caps = r.Capabilities()
	if len(caps) != 2 {
		t.Errorf("Capabilities() len = %d, want 2", len(caps))
	}

	// Check both are present
	capMap := make(map[string]bool)
	for _, c := range caps {
		capMap[c] = true
	}
	if !capMap["exec1"] || !capMap["exec2"] {
		t.Errorf("Capabilities() = %v, want [exec1, exec2]", caps)
	}
}

func TestRegistry_Execute(t *testing.T) {
	r := NewRegistry()

	expectedResult := Result{Output: []byte("test output"), ExitCode: 0}
	r.Register(&mockExecutor{name: "test", result: expectedResult})

	task := Task{ID: "1", Type: "test", Payload: []byte("{}")}
	result, err := r.Execute(context.Background(), task)

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(result.Output) != "test output" {
		t.Errorf("Output = %q, want %q", result.Output, "test output")
	}
}

func TestRegistry_Execute_UnknownType(t *testing.T) {
	r := NewRegistry()

	task := Task{ID: "1", Type: "unknown", Payload: []byte("{}")}
	_, err := r.Execute(context.Background(), task)

	if err == nil {
		t.Fatal("Execute() expected error for unknown type")
	}
	if err.Error() != "unknown executor type: unknown" {
		t.Errorf("Execute() error = %q", err.Error())
	}
}

func TestRegistry_Execute_Error(t *testing.T) {
	r := NewRegistry()

	execErr := errors.New("execution failed")
	r.Register(&mockExecutor{name: "test", executeErr: execErr})

	task := Task{ID: "1", Type: "test", Payload: []byte("{}")}
	_, err := r.Execute(context.Background(), task)

	if err != execErr {
		t.Errorf("Execute() error = %v, want %v", err, execErr)
	}
}

func TestRegistry_Validate(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockExecutor{name: "test", validateErr: nil})

	err := r.Validate("test", []byte("{}"))
	if err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestRegistry_Validate_Error(t *testing.T) {
	r := NewRegistry()
	validationErr := errors.New("invalid payload")
	r.Register(&mockExecutor{name: "test", validateErr: validationErr})

	err := r.Validate("test", []byte("{}"))
	if err != validationErr {
		t.Errorf("Validate() error = %v, want %v", err, validationErr)
	}
}

func TestRegistry_Validate_UnknownType(t *testing.T) {
	r := NewRegistry()

	err := r.Validate("unknown", []byte("{}"))
	if err == nil {
		t.Fatal("Validate() expected error for unknown type")
	}
}

func TestRegistry_HealthCheckAll(t *testing.T) {
	r := NewRegistry()

	healthErr := errors.New("unhealthy")
	r.Register(&mockExecutor{name: "healthy", healthErr: nil})
	r.Register(&mockExecutor{name: "unhealthy", healthErr: healthErr})

	results := r.HealthCheckAll(context.Background())

	if len(results) != 2 {
		t.Fatalf("HealthCheckAll() returned %d results, want 2", len(results))
	}

	if results["healthy"] != nil {
		t.Errorf("healthy executor health check = %v, want nil", results["healthy"])
	}
	if results["unhealthy"] != healthErr {
		t.Errorf("unhealthy executor health check = %v, want %v", results["unhealthy"], healthErr)
	}
}

func TestNewDefaultRegistry(t *testing.T) {
	r := NewDefaultRegistry()

	// Should have command executor
	e, ok := r.Get("command")
	if !ok {
		t.Fatal("NewDefaultRegistry() doesn't have command executor")
	}
	if e.Name() != "command" {
		t.Errorf("command executor name = %q, want %q", e.Name(), "command")
	}
}

func TestRegistry_Integration(t *testing.T) {
	// Integration test: use real command executor
	r := NewDefaultRegistry()

	payload, _ := json.Marshal(CommandPayload{Command: "echo", Args: []string{"integration"}})
	task := Task{ID: "int-test", Type: "command", Payload: payload}

	// Validate first
	if err := r.Validate("command", payload); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Execute
	result, err := r.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if string(result.Output) != "integration\n" {
		t.Errorf("Output = %q, want %q", result.Output, "integration\n")
	}
}
