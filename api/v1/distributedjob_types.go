// Package v1 contains API types for the distexec.io API group.
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DistributedJobSpec defines the desired state of DistributedJob.
type DistributedJobSpec struct {
	// Command to execute
	Command string `json:"command"`

	// Args are command arguments
	// +optional
	Args []string `json:"args,omitempty"`

	// Parallelism is the number of parallel tasks
	// +kubebuilder:default=1
	// +optional
	Parallelism int32 `json:"parallelism,omitempty"`

	// Timeout for the job (e.g., "30s", "5m", "1h")
	// +kubebuilder:default="30s"
	// +optional
	Timeout string `json:"timeout,omitempty"`

	// WorkingDir is the working directory for the command
	// +optional
	WorkingDir string `json:"workingDir,omitempty"`

	// Env contains environment variables
	// +optional
	Env map[string]string `json:"env,omitempty"`
}

// JobPhase represents the phase of a DistributedJob.
type JobPhase string

const (
	JobPhasePending   JobPhase = "Pending"
	JobPhaseRunning   JobPhase = "Running"
	JobPhaseSucceeded JobPhase = "Succeeded"
	JobPhaseFailed    JobPhase = "Failed"
)

// DistributedJobStatus defines the observed state of DistributedJob.
type DistributedJobStatus struct {
	// Phase is the current phase of the job
	Phase JobPhase `json:"phase,omitempty"`

	// StartTime is the time the job started
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is the time the job completed
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Completions is the number of completed tasks
	Completions int32 `json:"completions,omitempty"`

	// Failed is the number of failed tasks
	Failed int32 `json:"failed,omitempty"`

	// Message provides additional status information
	// +optional
	Message string `json:"message,omitempty"`

	// TaskIDs tracks the IDs of created tasks
	// +optional
	TaskIDs []string `json:"taskIds,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Completions",type=string,JSONPath=`.status.completions`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DistributedJob is the Schema for the distributedjobs API.
type DistributedJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DistributedJobSpec   `json:"spec,omitempty"`
	Status DistributedJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DistributedJobList contains a list of DistributedJob.
type DistributedJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DistributedJob `json:"items"`
}
