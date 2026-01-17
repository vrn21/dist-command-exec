package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/durationpb"

	pb "dist-command-exec/gen/go/distexec/v1"
	"dist-command-exec/pkg/executor"
)

var (
	masterAddr string
	outputJSON bool
	timeout    time.Duration
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "distctl",
		Short: "CLI for distributed command execution",
		Long:  "distctl is a CLI tool for submitting and monitoring jobs in the distributed command execution system.",
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&masterAddr, "master", "m", "localhost:50051", "Master node address")
	rootCmd.PersistentFlags().BoolVar(&outputJSON, "json", false, "Output in JSON format")

	// Add commands
	rootCmd.AddCommand(runCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(cancelCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [command] [args...]",
		Short: "Submit a command for execution",
		Long:  "Submit a shell command to be executed on a worker node.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommand(args[0], args[1:])
		},
	}

	cmd.Flags().DurationVarP(&timeout, "timeout", "t", 30*time.Second, "Command timeout")

	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [job-id]",
		Short: "Get status of a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getStatus(args[0])
		},
	}
}

func listCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listJobs(limit)
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "Maximum jobs to list")

	return cmd
}

func cancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel [job-id]",
		Short: "Cancel a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cancelJob(args[0])
		},
	}
}

func getClient() (pb.MasterServiceClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect: %w", err)
	}
	return pb.NewMasterServiceClient(conn), conn, nil
}

func runCommand(command string, args []string) error {
	client, conn, err := getClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	// Build payload
	payload := executor.CommandPayload{
		Command: command,
		Args:    args,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Submit job
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.Submit(ctx, &pb.SubmitRequest{
		TaskType: "command",
		Payload:  payloadBytes,
		Timeout:  durationpb.New(timeout),
	})
	if err != nil {
		return fmt.Errorf("submit failed: %w", err)
	}

	if outputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]string{"job_id": resp.JobId})
	}

	fmt.Printf("Job submitted: %s\n", resp.JobId)
	fmt.Println("Use 'distctl status " + resp.JobId + "' to check status")
	return nil
}

func getStatus(jobID string) error {
	client, conn, err := getClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.GetStatus(ctx, &pb.GetStatusRequest{JobId: jobID})
	if err != nil {
		return fmt.Errorf("get status failed: %w", err)
	}

	if outputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	fmt.Printf("Job ID:    %s\n", resp.JobId)
	fmt.Printf("Status:    %s\n", resp.Status.String())
	if resp.WorkerId != "" {
		fmt.Printf("Worker:    %s\n", resp.WorkerId)
	}
	if resp.CreatedAt != nil {
		fmt.Printf("Created:   %s\n", resp.CreatedAt.AsTime().Format(time.RFC3339))
	}
	if resp.CompletedAt != nil {
		fmt.Printf("Completed: %s\n", resp.CompletedAt.AsTime().Format(time.RFC3339))
	}
	if len(resp.Output) > 0 {
		fmt.Printf("\nOutput:\n%s", string(resp.Output))
	}
	if resp.Error != "" {
		fmt.Printf("\nError: %s\n", resp.Error)
	}
	if resp.ExitCode != 0 {
		fmt.Printf("Exit Code: %d\n", resp.ExitCode)
	}

	return nil
}

func listJobs(limit int) error {
	client, conn, err := getClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.ListJobs(ctx, &pb.ListJobsRequest{Limit: int32(limit)})
	if err != nil {
		return fmt.Errorf("list jobs failed: %w", err)
	}

	if outputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	if len(resp.Jobs) == 0 {
		fmt.Println("No jobs found")
		return nil
	}

	fmt.Printf("%-36s  %-10s  %-12s  %s\n", "JOB ID", "TYPE", "STATUS", "CREATED")
	fmt.Println("-------------------------------------------------------------------------------------")
	for _, job := range resp.Jobs {
		created := "N/A"
		if job.CreatedAt != nil {
			created = job.CreatedAt.AsTime().Format(time.RFC3339)
		}
		fmt.Printf("%-36s  %-10s  %-12s  %s\n",
			job.JobId,
			job.TaskType,
			job.Status.String(),
			created,
		)
	}
	fmt.Printf("\nTotal: %d\n", resp.Total)

	return nil
}

func cancelJob(jobID string) error {
	client, conn, err := getClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.Cancel(ctx, &pb.CancelRequest{JobId: jobID})
	if err != nil {
		return fmt.Errorf("cancel failed: %w", err)
	}

	if outputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	if resp.Cancelled {
		fmt.Printf("Job %s cancelled\n", jobID)
	} else {
		fmt.Printf("Could not cancel job: %s\n", resp.Message)
	}

	return nil
}
