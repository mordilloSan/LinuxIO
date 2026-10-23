package dockerupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/durabletask"
)

const (
	// UpdateRoute is the durable-task route of a container update.
	UpdateRoute = "docker.update_container"
	// ExecutorKind and ExecutorIdentity are recorded on the durable task when
	// the bridge launches the worker and checked again before the worker mutates.
	ExecutorKind     = "systemd-transient-unit"
	ExecutorIdentity = "root:linuxio-docker-update"
)

var (
	dockerUpdateStoreRoot      = durabletask.DefaultRoot
	runDockerContainerMutation = updateContainerWithProgress
)

var errDockerUpdateCancellationRequested = errors.New("docker update cancellation was requested before mutation")

// RequestFingerprint binds a durable update record to its target container.
func RequestFingerprint(containerID string) string {
	return durabletask.Fingerprint(UpdateRoute, "container="+containerID)
}

// UnitName is the transient systemd unit that runs one durable update.
func UnitName(operationID string) string {
	return "linuxio-docker-update-" + strings.ReplaceAll(operationID, "-", "") + ".service"
}

// UnitDescription is the description the bridge expects when it inspects the
// unit, which ties the unit to its operation and owner.
func UnitDescription(operationID string, uid uint32) string {
	return fmt.Sprintf("LinuxIO Docker update %s owned by UID %d", operationID, uid)
}

// BoundedMessage trims message to a size safe to persist and never returns it
// empty.
func BoundedMessage(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 1024 {
		return message[:1024]
	}
	if message == "" {
		return "durable Docker update failed"
	}
	return message
}

// ValidateTarget checks a container ID taken from a request or a persisted
// record.
func ValidateTarget(target string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("container ID is required")
	}
	if target != strings.TrimSpace(target) {
		return errors.New("container ID must not have surrounding whitespace")
	}
	if len(target) > 256 {
		return errors.New("container ID is too long")
	}
	for _, char := range target {
		if char < 0x20 || char == 0x7f {
			return errors.New("container ID contains an invalid control character")
		}
	}
	return nil
}

// ValidatePersisted checks that a durable record's target and request
// fingerprint agree, so a tampered record is never executed.
func ValidatePersisted(record durabletask.Record) error {
	if err := ValidateTarget(record.Target); err != nil {
		return fmt.Errorf("invalid persisted Docker update target: %w", err)
	}
	if record.RequestFingerprint != RequestFingerprint(record.Target) {
		return errors.New("durable Docker update request fingerprint does not match its target")
	}
	return nil
}

// ValidateExecutor checks that a durable record names the worker unit the
// bridge launches for it.
func ValidateExecutor(record durabletask.Record) error {
	if record.Executor.Kind != ExecutorKind || record.Executor.Identity != ExecutorIdentity || record.Executor.Handle != UnitName(record.ID) {
		return errors.New("durable Docker update has an unexpected executor identity")
	}
	return nil
}

// RunDurable is the root-owned worker entry point. It validates the persisted
// route/executor identity before reading the target and writes a typed result
// artifact for bridge reconciliation.
func RunDurable(ctx context.Context, operationID string) error {
	if err := durabletask.ValidateID(operationID); err != nil {
		return err
	}
	store := durabletask.NewStore(dockerUpdateStoreRoot)
	executor := durabletask.Executor{Kind: ExecutorKind, Handle: UnitName(operationID), Identity: ExecutorIdentity}
	record, err := store.GetForExecutor(ctx, operationID, UpdateRoute, executor)
	if err != nil {
		return fmt.Errorf("load durable Docker update %s: %w", operationID, err)
	}
	if record.Terminal() {
		return nil
	}
	if record.State != durabletask.StateLaunching && record.State != durabletask.StateRunning {
		return fmt.Errorf("durable Docker update %s is not launchable in state %q", operationID, record.State)
	}
	if validationErr := ValidatePersisted(record); validationErr != nil {
		return validationErr
	}
	record, err = claimDockerUpdateWorker(ctx, store, record)
	if errors.Is(err, errDockerUpdateCancellationRequested) {
		return writeDockerUpdateExecutorResult(
			store,
			operationID,
			DockerContainerUpdateResult{},
			context.Canceled,
		)
	}
	if err != nil {
		return err
	}

	report := func(phase, message string) {
		if _, progressErr := store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
			current.AppendProgress(time.Now().UTC(), phase, message)
			return nil
		}); progressErr != nil {
			slog.Warn(
				"failed to persist Docker update progress",
				"component", "docker",
				"subsystem", "update",
				"operation_id", record.ID,
				"phase", phase,
				"error", progressErr,
			)
		}
	}
	result, updateErr := runDockerContainerMutation(ctx, record.Target, report)
	return writeDockerUpdateExecutorResult(store, operationID, result, updateErr)
}

func claimDockerUpdateWorker(
	ctx context.Context,
	store *durabletask.Store,
	record durabletask.Record,
) (durabletask.Record, error) {
	now := time.Now().UTC()
	return store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		if err := ValidatePersisted(*current); err != nil {
			return err
		}
		if err := ValidateExecutor(*current); err != nil {
			return err
		}
		if current.CancelRequestedAt != nil {
			return errDockerUpdateCancellationRequested
		}
		if current.State != durabletask.StateLaunching && current.State != durabletask.StateRunning {
			return fmt.Errorf("durable Docker update %s is not launchable in state %q", current.ID, current.State)
		}
		current.State = durabletask.StateRunning
		if current.StartedAt == nil {
			current.StartedAt = &now
		}
		current.AppendProgress(now, "worker_started", "The system-owned Docker update worker claimed the mutation")
		return nil
	})
}

func writeDockerUpdateExecutorResult(
	store *durabletask.Store,
	operationID string,
	result DockerContainerUpdateResult,
	updateErr error,
) error {
	state := durabletask.StateCompleted
	exitCode := 0
	errorMessage := ""
	if updateErr != nil {
		result.Error = BoundedMessage(updateErr.Error())
		exitCode = 1
		errorMessage = result.Error
		state = durabletask.StateFailed
		if errors.Is(updateErr, context.Canceled) {
			state = durabletask.StateCanceled
			exitCode = 143
			errorMessage = "operation canceled"
		}
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return fmt.Errorf("encode durable Docker update result: %w", marshalErr)
	}
	executorResult := durabletask.ExecutorResult{ID: operationID, State: state, ExitCode: exitCode, FinishedAt: time.Now().UTC(), Result: payload, Error: errorMessage}
	data, marshalErr := json.Marshal(executorResult)
	if marshalErr != nil {
		return fmt.Errorf("encode durable Docker executor result: %w", marshalErr)
	}
	if _, writeErr := store.WriteArtifact(operationID, "executor-result.json", data, 0o600); writeErr != nil {
		return writeErr
	}
	return updateErr
}
