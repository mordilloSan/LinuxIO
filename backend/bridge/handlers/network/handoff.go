package network

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network/internal/hostbridge"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const bridgeHandoffRoute = "network.start_bridge_handoff"

const (
	bridgeHandoffStartTimeout    = 60 * time.Second
	bridgeHandoffDecisionTimeout = 30 * time.Second
)

// A handoff job stays running from its start until a decision or the native
// rollback window ends it; its checkpoint records the phase it is in.
const (
	handoffPhaseStarting  = "starting"
	handoffPhaseLaunching = "launching"
	handoffPhaseRunning   = "running"
)

var errHandoffPhaseChanged = errors.New("network handoff phase changed")

var (
	prepareBridgeHandoff = hostbridge.PrepareBridgeHandoff
	applyBridgeHandoff   = hostbridge.ApplyBridgeHandoff
	confirmBridgeHandoff = hostbridge.ConfirmBridgeHandoff
	revertBridgeHandoff  = hostbridge.RevertBridgeHandoff
)

type bridgeHandoffService interface {
	Start(context.Context, jobs.Owner, apischema.NetworkBridgeHandoffRequest) (apischema.NetworkBridgeHandoffStatus, error)
	Status(context.Context, uint32, string) (apischema.NetworkBridgeHandoffStatus, error)
	Confirm(context.Context, uint32, string) (apischema.NetworkBridgeHandoffStatus, error)
	Revert(context.Context, uint32, string) (apischema.NetworkBridgeHandoffStatus, error)
}

type networkHandlers struct {
	rt      runtime.Runtime
	handoff bridgeHandoffService
}

func newBridgeHandoffAdapter(_ runtime.Runtime) bridgeHandoffService {
	return &durableBridgeHandoffService{
		env: bridgeEnv,
		// The handoff runs only in the root bridge, so its job is root's.
		store: jobs.NewStore(version.DataDir+"/jobs/0", version.RunDir+"/jobs/0"),
		now:   func() time.Time { return time.Now().UTC() },
	}
}

type durableBridgeHandoffService struct {
	env   hostbridge.Environment
	store *jobs.Store
	now   func() time.Time
}

type bridgeHandoffRecord struct {
	Phase               string                        `json:"phase"`
	State               hostbridge.BridgeHandoffState `json:"state"`
	Deadline            time.Time                     `json:"deadline"`
	SafeReleaseDeadline time.Time                     `json:"safe_release_deadline"`
	DecisionStartedAt   time.Time                     `json:"decision_started_at,omitzero"`
}

func (s *durableBridgeHandoffService) Start(ctx context.Context, owner jobs.Owner, req apischema.NetworkBridgeHandoffRequest) (apischema.NetworkBridgeHandoffStatus, error) {
	if err := ctx.Err(); err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	networkMutationMu.Lock()
	defer networkMutationMu.Unlock()
	operationCtx, cancel := jobs.DetachedContext(bridgeHandoffStartTimeout)
	defer cancel()

	req.OperationID = strings.TrimSpace(req.OperationID)
	req.Name = strings.TrimSpace(req.Name)
	req.Member = strings.TrimSpace(req.Member)
	record, created, err := s.claimRecord(operationCtx, owner, req)
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	if !created {
		return s.status(operationCtx, record)
	}

	state, err := prepareBridgeHandoff(operationCtx, s.env, hostbridge.BridgeHandoffPlan{
		Name: req.Name, Member: req.Member, ConsoleAcknowledged: req.ConsoleAcknowledged,
	})
	if err != nil {
		s.failRecord(operationCtx, record, err)
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("prepare network handoff: %w", err)
	}
	now := s.now().UTC()
	data := bridgeHandoffRecord{
		Phase:               handoffPhaseStarting,
		State:               state,
		Deadline:            now.Add(hostbridge.BridgeHandoffConfirmationTimeout),
		SafeReleaseDeadline: now.Add(bridgeHandoffStartTimeout + hostbridge.BridgeHandoffConfirmationTimeout),
	}
	record, err = s.updateData(operationCtx, record, data, func(current *jobs.Record) {
		setHandoffProgress(current, "applying", "Moving the host IP configuration to the bridge")
	})
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("record network handoff: %w", err)
	}
	if applyErr := applyBridgeHandoff(operationCtx, s.env, &data.State); applyErr != nil {
		s.retainFailedStart(operationCtx, record, applyErr)
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("apply network handoff: %w", applyErr)
	}
	data.Phase = handoffPhaseRunning
	record, err = s.updateData(operationCtx, record, data, func(current *jobs.Record) {
		current.Executor = jobs.Executor{Kind: data.State.Backend, Unit: data.State.Handle}
		setHandoffProgress(current, "awaiting_confirmation", "Network handoff applied; waiting for confirmation")
	})
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("persist native network transaction: %w", err)
	}
	return s.status(operationCtx, record)
}

func (s *durableBridgeHandoffService) claimRecord(ctx context.Context, owner jobs.Owner, req apischema.NetworkBridgeHandoffRequest) (jobs.Record, bool, error) {
	// Releasing expired handoffs is not atomic with the create below, which is
	// acceptable: handoffs are rare and run only in the root bridge.
	if err := s.expireStale(ctx); err != nil {
		return jobs.Record{}, false, fmt.Errorf("claim durable network handoff: %w", err)
	}
	request, err := json.Marshal(req)
	if err != nil {
		return jobs.Record{}, false, fmt.Errorf("encode network handoff request: %w", err)
	}
	now := s.now().UTC()
	record := jobs.Record{
		ID:        req.OperationID,
		Route:     bridgeHandoffRoute,
		Owner:     owner,
		Target:    []string{req.Member},
		State:     jobs.StateRunning,
		CreatedAt: now,
		StartedAt: now,
		Request:   request,
		Protocol:  jobs.Protocol,
	}
	err = s.store.CreateUnlessActive(ctx, record)
	if errors.Is(err, jobs.ErrExists) {
		existing, getErr := s.store.Get(ctx, record.ID)
		if getErr != nil {
			return jobs.Record{}, false, fmt.Errorf("claim durable network handoff: %w", getErr)
		}
		var existingReq apischema.NetworkBridgeHandoffRequest
		if existing.Route != bridgeHandoffRoute || existing.Owner.UID != owner.UID || json.Unmarshal(existing.Request, &existingReq) != nil || existingReq != req {
			return jobs.Record{}, false, fmt.Errorf("%w: operation ID was already used for another network handoff", bridgeipc.ErrConflict)
		}
		return existing, false, nil
	}
	if errors.Is(err, jobs.ErrActive) {
		return jobs.Record{}, false, fmt.Errorf("%w: another network handoff is already active", bridgeipc.ErrConflict)
	}
	if err != nil {
		return jobs.Record{}, false, fmt.Errorf("claim durable network handoff: %w", err)
	}
	return record, true, nil
}

// expireStale settles every running handoff whose native rollback window has
// ended, so it no longer holds the route.
func (s *durableBridgeHandoffService) expireStale(ctx context.Context) error {
	active, err := s.store.ListActive(ctx)
	if err != nil {
		return err
	}
	for _, record := range active {
		if record.Route != bridgeHandoffRoute {
			continue
		}
		if _, err := s.expire(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

func (s *durableBridgeHandoffService) updateData(ctx context.Context, record jobs.Record, data bridgeHandoffRecord, update func(*jobs.Record)) (jobs.Record, error) {
	return s.store.Update(ctx, record.ID, func(current *jobs.Record) error {
		update(current)
		return setHandoffCheckpoint(current, data)
	})
}

func (s *durableBridgeHandoffService) Status(ctx context.Context, uid uint32, id string) (apischema.NetworkBridgeHandoffStatus, error) {
	record, err := s.getRecord(ctx, uid, id)
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	return s.status(ctx, record)
}

func (s *durableBridgeHandoffService) getRecord(ctx context.Context, uid uint32, id string) (jobs.Record, error) {
	record, err := s.store.Get(ctx, id)
	if errors.Is(err, jobs.ErrNotFound) || err == nil && (record.Route != bridgeHandoffRoute || record.Owner.UID != uid) {
		return jobs.Record{}, fmt.Errorf("%w: network handoff", bridgeipc.ErrNotFound)
	}
	return record, err
}

func (s *durableBridgeHandoffService) status(ctx context.Context, record jobs.Record) (apischema.NetworkBridgeHandoffStatus, error) {
	record, err := s.expire(ctx, record)
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	data, _ := decodeBridgeHandoffRecord(record)
	return handoffStatusFromRecord(record, data), nil
}

func (s *durableBridgeHandoffService) expire(ctx context.Context, record jobs.Record) (jobs.Record, error) {
	now := s.now().UTC()
	if data, _ := decodeBridgeHandoffRecord(record); !handoffExpired(record, data, now) {
		return record, nil
	}
	return s.store.Update(ctx, record.ID, func(current *jobs.Record) error {
		if data, _ := decodeBridgeHandoffRecord(*current); handoffExpired(*current, data, now) {
			expireHandoffRecord(current, data, now)
		}
		return nil
	})
}

func handoffExpired(record jobs.Record, data bridgeHandoffRecord, now time.Time) bool {
	if record.State != jobs.StateRunning {
		return false
	}
	deadline := data.SafeReleaseDeadline
	if deadline.IsZero() {
		// No native transaction was recorded: release the handoff once its
		// start could no longer be running.
		deadline = record.CreatedAt.Add(bridgeHandoffStartTimeout + hostbridge.BridgeHandoffConfirmationTimeout)
	}
	if decisionDeadline := data.DecisionStartedAt.Add(bridgeHandoffDecisionTimeout); data.Phase == handoffPhaseLaunching && decisionDeadline.After(deadline) {
		deadline = decisionDeadline
	}
	return !now.Before(deadline)
}

func expireHandoffRecord(record *jobs.Record, data bridgeHandoffRecord, finished time.Time) {
	if record.Error != nil || data.Phase == handoffPhaseLaunching {
		record.State = jobs.StateFailed
		if record.Error == nil {
			record.Error = &jobs.Error{Code: 500, Message: "network handoff decision outcome is unknown; inspect the host bridge"}
		}
		setHandoffProgress(record, "unknown", "Native network recovery could not be verified; inspect the host bridge")
	} else {
		record.State = jobs.StateCanceled
		setHandoffProgress(record, "reverted", "The native rollback window elapsed")
	}
	record.FinishedAt = finished
}

func (s *durableBridgeHandoffService) Confirm(ctx context.Context, uid uint32, id string) (apischema.NetworkBridgeHandoffStatus, error) {
	return s.finish(ctx, uid, id, "confirming", jobs.StateCompleted, confirmBridgeHandoff)
}

func (s *durableBridgeHandoffService) Revert(ctx context.Context, uid uint32, id string) (apischema.NetworkBridgeHandoffStatus, error) {
	return s.finish(ctx, uid, id, "reverting", jobs.StateCanceled, revertBridgeHandoff)
}

func (s *durableBridgeHandoffService) finish(ctx context.Context, uid uint32, id, phase string, terminal jobs.State, action func(context.Context, hostbridge.Environment, *hostbridge.BridgeHandoffState) error) (apischema.NetworkBridgeHandoffStatus, error) {
	if err := ctx.Err(); err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	networkMutationMu.Lock()
	defer networkMutationMu.Unlock()
	operationCtx, cancel := jobs.DetachedContext(bridgeHandoffDecisionTimeout)
	defer cancel()

	record, err := s.getRecord(operationCtx, uid, id)
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	data, err := decodeBridgeHandoffRecord(record)
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	record, err = s.expire(operationCtx, record)
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	if record.State == terminal {
		return handoffStatusFromRecord(record, data), nil
	}
	if record.State != jobs.StateRunning || data.Phase != handoffPhaseRunning {
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("%w: network handoff is no longer awaiting a decision", bridgeipc.ErrConflict)
	}
	if !s.now().UTC().Before(data.Deadline) {
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("%w: network handoff decision window has elapsed", bridgeipc.ErrConflict)
	}
	now := s.now().UTC()
	_, err = s.store.Update(operationCtx, id, func(current *jobs.Record) error {
		currentData, ok := handoffInPhase(*current, handoffPhaseRunning)
		if !ok {
			return errHandoffPhaseChanged
		}
		currentData.Phase, currentData.DecisionStartedAt = handoffPhaseLaunching, now
		setHandoffProgress(current, phase, "Network handoff "+phase)
		return setHandoffCheckpoint(current, currentData)
	})
	if errors.Is(err, errHandoffPhaseChanged) {
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("%w: another network handoff decision is already in progress", bridgeipc.ErrConflict)
	}
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	if actionErr := action(operationCtx, s.env, &data.State); actionErr != nil {
		s.retainFailedDecision(operationCtx, id, actionErr)
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("%s network handoff: %w", strings.TrimSuffix(phase, "ing"), actionErr)
	}
	finished := s.now().UTC()
	record, err = s.store.Update(operationCtx, id, func(current *jobs.Record) error {
		if _, ok := handoffInPhase(*current, handoffPhaseLaunching); !ok {
			return errHandoffPhaseChanged
		}
		current.State = terminal
		current.Error = nil
		current.FinishedAt = finished
		message := "The network handoff was reverted"
		if terminal == jobs.StateCompleted {
			message = "The network handoff was confirmed"
		}
		setHandoffProgress(current, string(terminal), message)
		return nil
	})
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("record network handoff decision: %w", err)
	}
	return handoffStatusFromRecord(record, data), nil
}

// retainFailedDecision returns a handoff whose decision failed to awaiting a
// decision, keeping the error for the expiry to report.
func (s *durableBridgeHandoffService) retainFailedDecision(ctx context.Context, id string, actionErr error) {
	_, _ = s.store.Update(ctx, id, func(current *jobs.Record) error {
		data, ok := handoffInPhase(*current, handoffPhaseLaunching)
		if !ok {
			return nil
		}
		data.Phase = handoffPhaseRunning
		current.Error = &jobs.Error{Code: 500, Message: actionErr.Error()}
		setHandoffProgress(current, "awaiting_confirmation", actionErr.Error())
		return setHandoffCheckpoint(current, data)
	})
}

func (s *durableBridgeHandoffService) failRecord(ctx context.Context, record jobs.Record, operationErr error) {
	finished := s.now().UTC()
	_, _ = s.store.Update(ctx, record.ID, func(current *jobs.Record) error {
		current.State = jobs.StateFailed
		current.FinishedAt = finished
		current.Error = &jobs.Error{Code: 500, Message: operationErr.Error()}
		setHandoffProgress(current, "failed", operationErr.Error())
		return nil
	})
}

func (s *durableBridgeHandoffService) retainFailedStart(ctx context.Context, record jobs.Record, operationErr error) {
	_, _ = s.store.Update(ctx, record.ID, func(current *jobs.Record) error {
		if current.State != jobs.StateRunning {
			return nil
		}
		current.Error = &jobs.Error{Code: 500, Message: operationErr.Error()}
		setHandoffProgress(current, "failed", operationErr.Error())
		return nil
	})
}

// decodeBridgeHandoffRecord returns the zero record with any error.
func decodeBridgeHandoffRecord(record jobs.Record) (bridgeHandoffRecord, error) {
	var data bridgeHandoffRecord
	if len(record.Checkpoint) == 0 {
		return data, errors.New("network handoff has no native transaction record")
	}
	if err := json.Unmarshal(record.Checkpoint, &data); err != nil {
		return bridgeHandoffRecord{}, fmt.Errorf("decode network handoff record: %w", err)
	}
	return data, nil
}

// handoffInPhase reports whether the handoff is running in phase, with its checkpoint.
func handoffInPhase(record jobs.Record, phase string) (bridgeHandoffRecord, bool) {
	data, err := decodeBridgeHandoffRecord(record)
	return data, err == nil && record.State == jobs.StateRunning && data.Phase == phase
}

func setHandoffCheckpoint(record *jobs.Record, data bridgeHandoffRecord) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	record.Checkpoint = encoded
	return nil
}

// setHandoffProgress shows the handoff's latest step in the jobs list; a
// message that cannot be encoded keeps the previous step.
func setHandoffProgress(record *jobs.Record, phase, message string) {
	if progress, err := json.Marshal(bridgeipc.TaskProgress{Phase: phase, Message: message}); err == nil {
		record.Progress = progress
	}
}

func handoffStatusFromRecord(record jobs.Record, data bridgeHandoffRecord) apischema.NetworkBridgeHandoffStatus {
	status := apischema.NetworkBridgeHandoffStatus{
		OperationID: record.ID,
		Name:        data.State.Plan.Name,
		Backend:     data.State.Backend,
	}
	if len(record.Target) > 0 {
		status.Member = record.Target[0]
	}
	if !data.Deadline.IsZero() {
		status.Deadline = data.Deadline.UTC().Format(time.RFC3339)
	}
	switch record.State {
	case jobs.StateRunning:
		switch data.Phase {
		case handoffPhaseLaunching:
			status.State = apischema.NetworkBridgeHandoffApplying
			status.Message = "Finishing the network handoff decision"
		case handoffPhaseRunning:
			status.State = apischema.NetworkBridgeHandoffAwaitingConfirmation
			status.Message = "The bridge is active; confirm it before the deadline"
		default: // starting, or not yet recorded
			status.State = apischema.NetworkBridgeHandoffApplying
			status.Message = "Applying the network handoff"
			if record.Error != nil {
				status.Error = record.Error.Message
				status.Message = "Network handoff failed while applying; waiting for native rollback"
			}
		}
	case jobs.StateCompleted:
		status.State = apischema.NetworkBridgeHandoffConfirmed
		status.Message = "The network handoff is confirmed"
	case jobs.StateCanceled:
		status.State = apischema.NetworkBridgeHandoffReverted
		status.Message = "The original network configuration was restored"
	default:
		status.State = apischema.NetworkBridgeHandoffUnknown
		status.Message = "The network handoff outcome is unknown"
		if record.Error != nil {
			status.Error = record.Error.Message
		}
	}
	return status
}

func validateHandoffOperationID(value string) error {
	if err := jobs.ValidateID(strings.TrimSpace(value)); err != nil {
		return fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	return nil
}

func validateHandoffStartRequest(req apischema.NetworkBridgeHandoffRequest) error {
	if err := validateHandoffOperationID(req.OperationID); err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("%w: bridge name is required", bridgeipc.ErrInvalidArgs)
	}
	if strings.TrimSpace(req.Member) == "" {
		return fmt.Errorf("%w: bridge member interface is required", bridgeipc.ErrInvalidArgs)
	}
	if !req.ConsoleAcknowledged {
		return fmt.Errorf("%w: console or out-of-band recovery must be acknowledged", bridgeipc.ErrInvalidArgs)
	}
	return nil
}

type validatingBridgeHandoffService struct{ inner bridgeHandoffService }

func (s validatingBridgeHandoffService) Start(ctx context.Context, owner jobs.Owner, req apischema.NetworkBridgeHandoffRequest) (apischema.NetworkBridgeHandoffStatus, error) {
	if err := validateHandoffStartRequest(req); err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	return s.inner.Start(ctx, owner, req)
}

func (s validatingBridgeHandoffService) Status(ctx context.Context, uid uint32, id string) (apischema.NetworkBridgeHandoffStatus, error) {
	if err := validateHandoffOperationID(id); err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	return s.inner.Status(ctx, uid, id)
}

func (s validatingBridgeHandoffService) Confirm(ctx context.Context, uid uint32, id string) (apischema.NetworkBridgeHandoffStatus, error) {
	if err := validateHandoffOperationID(id); err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	return s.inner.Confirm(ctx, uid, id)
}

func (s validatingBridgeHandoffService) Revert(ctx context.Context, uid uint32, id string) (apischema.NetworkBridgeHandoffStatus, error) {
	if err := validateHandoffOperationID(id); err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	return s.inner.Revert(ctx, uid, id)
}
