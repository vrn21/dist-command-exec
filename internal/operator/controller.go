// Package operator provides the Kubernetes operator for DistributedJob resources.
package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/durationpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	pb "dist-command-exec/gen/go/distexec/v1"
	"dist-command-exec/pkg/executor"
)

var (
	// GVR is the GroupVersionResource for DistributedJob
	GVR = schema.GroupVersionResource{
		Group:    "distexec.io",
		Version:  "v1",
		Resource: "distributedjobs",
	}
)

// Controller watches DistributedJob resources and reconciles them.
type Controller struct {
	client       dynamic.Interface
	masterAddr   string
	masterClient pb.MasterServiceClient
	masterConn   *grpc.ClientConn
	namespace    string
}

// NewController creates a new operator controller.
func NewController(client dynamic.Interface, masterAddr, namespace string) *Controller {
	return &Controller{
		client:     client,
		masterAddr: masterAddr,
		namespace:  namespace,
	}
}

// Run starts the controller.
func (c *Controller) Run(ctx context.Context) error {
	// Connect to master
	conn, err := grpc.NewClient(c.masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect to master: %w", err)
	}
	c.masterConn = conn
	c.masterClient = pb.NewMasterServiceClient(conn)
	defer conn.Close()

	// Create informer factory
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		c.client,
		30*time.Second,
		c.namespace,
		nil,
	)

	// Get informer for DistributedJob
	informer := factory.ForResource(GVR).Informer()

	// Add event handlers
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAdd,
		UpdateFunc: c.onUpdate,
		DeleteFunc: c.onDelete,
	})

	log.Info().Str("namespace", c.namespace).Msg("starting operator controller")

	// Start informer
	factory.Start(ctx.Done())

	// Wait for cache sync
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		return fmt.Errorf("failed to sync cache")
	}

	log.Info().Msg("cache synced, watching DistributedJob resources")

	<-ctx.Done()
	return nil
}

func (c *Controller) onAdd(obj interface{}) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return
	}

	log.Info().
		Str("name", u.GetName()).
		Str("namespace", u.GetNamespace()).
		Msg("DistributedJob added")

	c.reconcile(context.Background(), u)
}

func (c *Controller) onUpdate(oldObj, newObj interface{}) {
	u, ok := newObj.(*unstructured.Unstructured)
	if !ok {
		return
	}

	log.Debug().
		Str("name", u.GetName()).
		Msg("DistributedJob updated")

	c.reconcile(context.Background(), u)
}

func (c *Controller) onDelete(obj interface{}) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return
	}

	log.Info().
		Str("name", u.GetName()).
		Msg("DistributedJob deleted")
}

func (c *Controller) reconcile(ctx context.Context, u *unstructured.Unstructured) {
	name := u.GetName()
	_ = u.GetNamespace() // Used in startJobs/checkJobStatus via u

	// Get current phase
	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")

	// Skip if already completed or failed
	if phase == "Succeeded" || phase == "Failed" {
		return
	}

	// Extract spec
	spec, found, err := unstructured.NestedMap(u.Object, "spec")
	if err != nil || !found {
		log.Error().Err(err).Str("name", name).Msg("failed to get spec")
		return
	}

	command, _ := spec["command"].(string)
	args := toStringSlice(spec["args"])
	parallelism := int32(1)
	if p, ok := spec["parallelism"].(int64); ok {
		parallelism = int32(p)
	}
	timeoutStr, _ := spec["timeout"].(string)
	if timeoutStr == "" {
		timeoutStr = "30s"
	}
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		timeout = 30 * time.Second
	}

	// If pending, start jobs
	if phase == "" || phase == "Pending" {
		c.startJobs(ctx, u, command, args, parallelism, timeout)
		return
	}

	// If running, check status
	if phase == "Running" {
		c.checkJobStatus(ctx, u)
	}
}

func (c *Controller) startJobs(ctx context.Context, u *unstructured.Unstructured, command string, args []string, parallelism int32, timeout time.Duration) {
	name := u.GetName()
	namespace := u.GetNamespace()

	log.Info().
		Str("name", name).
		Str("command", command).
		Int32("parallelism", parallelism).
		Msg("starting distributed job")

	// Build payload
	payload := executor.CommandPayload{
		Command: command,
		Args:    args,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		c.updateStatus(ctx, namespace, name, "Failed", 0, 0, fmt.Sprintf("marshal error: %v", err), nil)
		return
	}

	// Submit tasks
	var taskIDs []string
	for i := int32(0); i < parallelism; i++ {
		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		resp, err := c.masterClient.Submit(reqCtx, &pb.SubmitRequest{
			TaskType: "command",
			Payload:  payloadBytes,
			Timeout:  durationpb.New(timeout),
			Metadata: map[string]string{
				"distributedjob": name,
				"namespace":      namespace,
				"task_index":     fmt.Sprintf("%d", i),
			},
		})
		cancel()

		if err != nil {
			log.Error().Err(err).Str("name", name).Msg("failed to submit task")
			continue
		}
		taskIDs = append(taskIDs, resp.JobId)
	}

	if len(taskIDs) == 0 {
		c.updateStatus(ctx, namespace, name, "Failed", 0, int32(parallelism), "failed to submit any tasks", nil)
		return
	}

	// Update status to Running
	c.updateStatus(ctx, namespace, name, "Running", 0, 0, fmt.Sprintf("submitted %d tasks", len(taskIDs)), taskIDs)
}

func (c *Controller) checkJobStatus(ctx context.Context, u *unstructured.Unstructured) {
	name := u.GetName()
	namespace := u.GetNamespace()

	// Get task IDs from status
	taskIDs, _, _ := unstructured.NestedStringSlice(u.Object, "status", "taskIds")
	if len(taskIDs) == 0 {
		return
	}

	// Check each task
	var completed, failed int32
	for _, taskID := range taskIDs {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		resp, err := c.masterClient.GetStatus(reqCtx, &pb.GetStatusRequest{JobId: taskID})
		cancel()

		if err != nil {
			continue
		}

		switch resp.Status {
		case pb.JobStatus_JOB_STATUS_COMPLETED:
			completed++
		case pb.JobStatus_JOB_STATUS_FAILED:
			failed++
		}
	}

	total := int32(len(taskIDs))

	// Determine overall phase
	if completed+failed >= total {
		if failed > 0 {
			c.updateStatus(ctx, namespace, name, "Failed", completed, failed, fmt.Sprintf("%d/%d failed", failed, total), taskIDs)
		} else {
			c.updateStatus(ctx, namespace, name, "Succeeded", completed, failed, "all tasks completed", taskIDs)
		}
	} else {
		// Still running, update progress
		c.updateStatus(ctx, namespace, name, "Running", completed, failed, fmt.Sprintf("%d/%d completed", completed, total), taskIDs)
	}
}

func (c *Controller) updateStatus(ctx context.Context, namespace, name, phase string, completions, failed int32, message string, taskIDs []string) {
	// Get current resource
	u, err := c.client.Resource(GVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.Error().Err(err).Str("name", name).Msg("failed to get resource for status update")
		return
	}

	// Update status
	status := map[string]interface{}{
		"phase":       phase,
		"completions": completions,
		"failed":      failed,
		"message":     message,
	}

	if taskIDs != nil {
		status["taskIds"] = taskIDs
	}

	// Set startTime on first update
	if phase == "Running" {
		existingStart, _, _ := unstructured.NestedString(u.Object, "status", "startTime")
		if existingStart == "" {
			status["startTime"] = time.Now().Format(time.RFC3339)
		}
	}

	// Set completionTime when done
	if phase == "Succeeded" || phase == "Failed" {
		status["completionTime"] = time.Now().Format(time.RFC3339)
	}

	// Set status on the object
	if err := unstructured.SetNestedField(u.Object, status, "status"); err != nil {
		log.Error().Err(err).Str("name", name).Msg("failed to set status field")
		return
	}

	// Update the resource
	_, err = c.client.Resource(GVR).Namespace(namespace).UpdateStatus(ctx, u, metav1.UpdateOptions{})
	if err != nil {
		log.Error().Err(err).Str("name", name).Msg("failed to update status")
		return
	}

	log.Info().
		Str("name", name).
		Str("phase", phase).
		Int32("completions", completions).
		Msg("status updated")
}

func toStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	slice, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// Ensure uuid is used (for potential future task ID generation)
var _ = uuid.New
