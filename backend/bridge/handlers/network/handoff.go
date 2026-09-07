package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	networkbackend "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network/internal/network"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/durabletask"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const bridgeHandoffRoute = "network.bridge_handoff"

const (
	bridgeHandoffStartTimeout    = 60 * time.Second
	bridgeHandoffDecisionTimeout = 30 * time.Second
)

var (
	prepareBridgeHandoff = networkbackend.PrepareBridgeHandoff
	applyBridgeHandoff   = networkbackend.ApplyBridgeHandoff
	confirmBridgeHandoff = networkbackend.ConfirmBridgeHandoff
	revertBridgeHandoff  = networkbackend.RevertBridgeHandoff
)

type bridgeHandoffService interface {
	Start(context.Context, uint32, apischema.NetworkBridgeHandoffRequest) (apischema.NetworkBridgeHandoffStatus, error)
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
		env:   networkEnv,
		store: durabletask.NewStore(durabletask.DefaultRoot),
		now:   func() time.Time { return time.Now().UTC() },
	}
}

type durableBridgeHandoffService struct {
	env   networkbackend.Environment
	store *durabletask.Store
	now   func() time.Time
}

type bridgeHandoffRecord struct {
	State               networkbackend.BridgeHandoffState `json:"state"`
	Deadline            time.Time                         `json:"deadline"`
	SafeReleaseDeadline time.Time                         `json:"safe_release_deadline"`
}

func (s *durableBridgeHandoffService) Start(ctx context.Context, uid uint32, req apischema.NetworkBridgeHandoffRequest) (apischema.NetworkBridgeHandoffStatus, error) {
	if err := ctx.Err(); err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	networkMutationMu.Lock()
	defer networkMutationMu.Unlock()
	operationCtx, cancel := durabletask.DetachedContext(bridgeHandoffStartTimeout)
	defer cancel()

	id := strings.TrimSpace(req.OperationID)
	name := strings.TrimSpace(req.Name)
	member := strings.TrimSpace(req.Member)
	record, created, err := s.claimRecord(operationCtx, uid, id, name, member)
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	if !created {
		return s.status(operationCtx, uid, record)
	}

	state, err := prepareBridgeHandoff(operationCtx, s.env, networkbackend.BridgeHandoffPlan{
		Name: name, Member: member, ConsoleAcknowledged: req.ConsoleAcknowledged,
	})
	if err != nil {
		s.failRecord(operationCtx, record, err)
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("prepare network handoff: %w", err)
	}
	now := s.now().UTC()
	data := bridgeHandoffRecord{
		State:               state,
		Deadline:            now.Add(networkbackend.BridgeHandoffConfirmationTimeout),
		SafeReleaseDeadline: now.Add(bridgeHandoffStartTimeout + networkbackend.BridgeHandoffConfirmationTimeout),
	}
	record, err = s.updateData(operationCtx, record, data, func(current *durabletask.Record) {
		current.AppendProgress(s.now(), "applying", "Moving the host IP configuration to the bridge")
	})
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("record network handoff: %w", err)
	}
	if applyErr := applyBridgeHandoff(operationCtx, s.env, &data.State); applyErr != nil {
		s.retainFailedStart(operationCtx, record, applyErr)
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("apply network handoff: %w", applyErr)
	}
	now = s.now().UTC()
	record, err = s.updateData(operationCtx, record, data, func(current *durabletask.Record) {
		current.State = durabletask.StateRunning
		current.Executor = durabletask.Executor{Kind: data.State.Backend, Handle: data.State.Handle, Identity: "system-bus"}
		current.StartedAt = &now
		current.AppendProgress(now, "awaiting_confirmation", "Network handoff applied; waiting for confirmation")
	})
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("persist native network transaction: %w", err)
	}
	return s.status(operationCtx, uid, record)
}

func (s *durableBridgeHandoffService) claimRecord(ctx context.Context, uid uint32, id, name, member string) (durabletask.Record, bool, error) {
	record, created, err := s.store.Claim(ctx, durabletask.Claim{
		ID:                 id,
		Route:              bridgeHandoffRoute,
		UID:                uid,
		RequestFingerprint: durabletask.Fingerprint(bridgeHandoffRoute, name+"\x00"+member),
		Target:             member,
		ExclusiveRoute:     true,
		ReconcileActive:    s.reconcileActiveRecord,
	})
	if errors.Is(err, durabletask.ErrConflict) {
		return durabletask.Record{}, false, bridgeipc.NewError("operation ID was already used for another network handoff", 409)
	}
	if errors.Is(err, durabletask.ErrActive) {
		return durabletask.Record{}, false, bridgeipc.NewError("another network handoff is already active", 409)
	}
	if err != nil {
		return durabletask.Record{}, false, fmt.Errorf("claim durable network handoff: %w", err)
	}
	return record, created, nil
}

func (s *durableBridgeHandoffService) updateData(ctx context.Context, record durabletask.Record, data bridgeHandoffRecord, update func(*durabletask.Record)) (durabletask.Record, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return durabletask.Record{}, err
	}
	return s.store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.Result = encoded
		if update != nil {
			update(current)
		}
		return nil
	})
}

func (s *durableBridgeHandoffService) Status(ctx context.Context, uid uint32, id string) (apischema.NetworkBridgeHandoffStatus, error) {
	record, err := s.getRecord(ctx, uid, id)
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	return s.status(ctx, uid, record)
}

func (s *durableBridgeHandoffService) getRecord(ctx context.Context, uid uint32, id string) (durabletask.Record, error) {
	record, err := s.store.Get(ctx, id, uid)
	if errors.Is(err, durabletask.ErrNotFound) || err == nil && record.Route != bridgeHandoffRoute {
		return durabletask.Record{}, bridgeipc.NewError("network handoff not found", 404)
	}
	return record, err
}

func (s *durableBridgeHandoffService) status(ctx context.Context, uid uint32, record durabletask.Record) (apischema.NetworkBridgeHandoffStatus, error) {
	data, err := decodeBridgeHandoffRecord(record)
	if err != nil {
		data = bridgeHandoffRecord{}
	}
	record, err = s.expire(ctx, uid, record, data)
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	return handoffStatusFromRecord(record, data), nil
}

func (s *durableBridgeHandoffService) expire(ctx context.Context, uid uint32, record durabletask.Record, data bridgeHandoffRecord) (durabletask.Record, error) {
	now := s.now().UTC()
	if !handoffExpired(record, data, now) {
		return record, nil
	}
	return s.store.Update(ctx, record.ID, uid, func(current *durabletask.Record) error {
		if current.Terminal() {
			return nil
		}
		currentData, decodeErr := decodeBridgeHandoffRecord(*current)
		if decodeErr != nil {
			currentData = bridgeHandoffRecord{}
		}
		if !handoffExpired(*current, currentData, now) {
			return nil
		}
		expireHandoffRecord(current, now)
		return nil
	})
}

func (s *durableBridgeHandoffService) reconcileActiveRecord(record *durabletask.Record, _ time.Time) error {
	now := s.now().UTC()
	data, err := decodeBridgeHandoffRecord(*record)
	if err != nil {
		data = bridgeHandoffRecord{}
	}
	if handoffExpired(*record, data, now) {
		expireHandoffRecord(record, now)
	}
	return nil
}

func handoffExpired(record durabletask.Record, data bridgeHandoffRecord, now time.Time) bool {
	if record.Terminal() {
		return false
	}
	deadline := data.SafeReleaseDeadline
	if deadline.IsZero() && data.Deadline.IsZero() {
		if record.State != durabletask.StateQueued {
			return false
		}
		deadline = record.CreatedAt.Add(bridgeHandoffStartTimeout + networkbackend.BridgeHandoffConfirmationTimeout)
	}
	if deadline.IsZero() {
		// Older records persisted the native deadline before the native start;
		// allow the bounded start context to finish before recovering them.
		deadline = data.Deadline.Add(bridgeHandoffStartTimeout)
	}
	if record.State == durabletask.StateLaunching {
		decisionDeadline := record.UpdatedAt.Add(bridgeHandoffDecisionTimeout)
		if decisionDeadline.After(deadline) {
			deadline = decisionDeadline
		}
	}
	if deadline.IsZero() {
		return false
	}
	return !now.Before(deadline)
}

func expireHandoffRecord(record *durabletask.Record, finished time.Time) {
	if record.Error != nil || record.State == durabletask.StateLaunching {
		record.State = durabletask.StateUnknown
		if record.Error == nil {
			record.Error = &durabletask.StructuredError{Code: 500, Message: "network handoff decision outcome is unknown; inspect the host bridge"}
		}
		record.AppendProgress(finished, "unknown", "Native network recovery could not be verified; inspect the host bridge")
	} else {
		record.State = durabletask.StateCanceled
		record.AppendProgress(finished, "reverted", "The native rollback window elapsed")
	}
	record.FinishedAt = &finished
}

func (s *durableBridgeHandoffService) Confirm(ctx context.Context, uid uint32, id string) (apischema.NetworkBridgeHandoffStatus, error) {
	return s.finish(ctx, uid, id, "confirming", durabletask.StateCompleted, confirmBridgeHandoff)
}

func (s *durableBridgeHandoffService) Revert(ctx context.Context, uid uint32, id string) (apischema.NetworkBridgeHandoffStatus, error) {
	return s.finish(ctx, uid, id, "reverting", durabletask.StateCanceled, func(ctx context.Context, _ networkbackend.Environment, state *networkbackend.BridgeHandoffState) error {
		return revertBridgeHandoff(ctx, state)
	})
}

func (s *durableBridgeHandoffService) finish(ctx context.Context, uid uint32, id, phase string, terminal durabletask.State, action func(context.Context, networkbackend.Environment, *networkbackend.BridgeHandoffState) error) (apischema.NetworkBridgeHandoffStatus, error) {
	if err := ctx.Err(); err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	networkMutationMu.Lock()
	defer networkMutationMu.Unlock()
	operationCtx, cancel := durabletask.DetachedContext(bridgeHandoffDecisionTimeout)
	defer cancel()

	record, err := s.getRecord(operationCtx, uid, id)
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	data, err := decodeBridgeHandoffRecord(record)
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	record, err = s.expire(operationCtx, uid, record, data)
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	if record.State == terminal {
		return handoffStatusFromRecord(record, data), nil
	}
	if record.State != durabletask.StateRunning {
		return apischema.NetworkBridgeHandoffStatus{}, bridgeipc.NewError("network handoff is no longer awaiting a decision", 409)
	}
	if !s.now().UTC().Before(data.Deadline) {
		return apischema.NetworkBridgeHandoffStatus{}, bridgeipc.NewError("network handoff decision window has elapsed", 409)
	}
	now := s.now().UTC()
	_, err = s.store.Update(operationCtx, id, uid, func(current *durabletask.Record) error {
		if current.State != durabletask.StateRunning {
			return durabletask.ErrConflict
		}
		current.State = durabletask.StateLaunching
		current.AppendProgress(now, phase, "Network handoff "+phase)
		return nil
	})
	if errors.Is(err, durabletask.ErrConflict) {
		return apischema.NetworkBridgeHandoffStatus{}, bridgeipc.NewError("another network handoff decision is already in progress", 409)
	}
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	if actionErr := action(operationCtx, s.env, &data.State); actionErr != nil {
		_, _ = s.store.Update(operationCtx, id, uid, func(current *durabletask.Record) error {
			if current.State == durabletask.StateLaunching {
				current.State = durabletask.StateRunning
				current.Error = &durabletask.StructuredError{Code: 500, Message: actionErr.Error()}
				current.AppendProgress(s.now(), "awaiting_confirmation", actionErr.Error())
			}
			return nil
		})
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("%s network handoff: %w", strings.TrimSuffix(phase, "ing"), actionErr)
	}
	finished := s.now().UTC()
	record, err = s.store.Update(operationCtx, id, uid, func(current *durabletask.Record) error {
		if current.State != durabletask.StateLaunching {
			return durabletask.ErrConflict
		}
		current.State = terminal
		current.Error = nil
		current.FinishedAt = &finished
		message := "The network handoff was reverted"
		if terminal == durabletask.StateCompleted {
			message = "The network handoff was confirmed"
		}
		current.AppendProgress(finished, string(terminal), message)
		return nil
	})
	if err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, fmt.Errorf("record network handoff decision: %w", err)
	}
	return handoffStatusFromRecord(record, data), nil
}

func (s *durableBridgeHandoffService) failRecord(ctx context.Context, record durabletask.Record, operationErr error) {
	finished := s.now().UTC()
	_, _ = s.store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		current.State = durabletask.StateFailed
		current.FinishedAt = &finished
		current.Error = &durabletask.StructuredError{Code: 500, Message: operationErr.Error()}
		current.AppendProgress(finished, "failed", operationErr.Error())
		return nil
	})
}

func (s *durableBridgeHandoffService) retainFailedStart(ctx context.Context, record durabletask.Record, operationErr error) {
	failed := s.now().UTC()
	_, _ = s.store.Update(ctx, record.ID, record.UID, func(current *durabletask.Record) error {
		if current.Terminal() {
			return nil
		}
		current.Error = &durabletask.StructuredError{Code: 500, Message: operationErr.Error()}
		current.AppendProgress(failed, "failed", operationErr.Error())
		return nil
	})
}

func decodeBridgeHandoffRecord(record durabletask.Record) (bridgeHandoffRecord, error) {
	var data bridgeHandoffRecord
	if len(record.Result) == 0 {
		return data, errors.New("network handoff has no native transaction record")
	}
	if err := json.Unmarshal(record.Result, &data); err != nil {
		return data, fmt.Errorf("decode network handoff record: %w", err)
	}
	return data, nil
}

func handoffStatusFromRecord(record durabletask.Record, data bridgeHandoffRecord) apischema.NetworkBridgeHandoffStatus {
	status := apischema.NetworkBridgeHandoffStatus{
		OperationID: record.ID,
		Name:        data.State.Plan.Name,
		Member:      record.Target,
		Backend:     data.State.Backend,
	}
	if !data.Deadline.IsZero() {
		status.Deadline = data.Deadline.UTC().Format(time.RFC3339)
	}
	switch record.State {
	case durabletask.StateQueued:
		status.State = apischema.NetworkBridgeHandoffApplying
		status.Message = "Applying the network handoff"
		if record.Error != nil {
			status.Error = record.Error.Message
			status.Message = "Network handoff failed while applying; waiting for native rollback"
		}
	case durabletask.StateLaunching:
		status.State = apischema.NetworkBridgeHandoffApplying
		status.Message = "Finishing the network handoff decision"
	case durabletask.StateRunning:
		status.State = apischema.NetworkBridgeHandoffAwaitingConfirmation
		status.Message = "The bridge is active; confirm it before the deadline"
	case durabletask.StateCompleted:
		status.State = apischema.NetworkBridgeHandoffConfirmed
		status.Message = "The network handoff is confirmed"
	case durabletask.StateCanceled:
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
	if err := durabletask.ValidateID(strings.TrimSpace(value)); err != nil {
		return bridgeipc.NewError(err.Error(), 400)
	}
	return nil
}

func validateHandoffStartRequest(req apischema.NetworkBridgeHandoffRequest) error {
	if err := validateHandoffOperationID(req.OperationID); err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" {
		return bridgeipc.NewError("bridge name is required", 400)
	}
	if strings.TrimSpace(req.Member) == "" {
		return bridgeipc.NewError("bridge member interface is required", 400)
	}
	if !req.ConsoleAcknowledged {
		return bridgeipc.NewError("console or out-of-band recovery must be acknowledged", 400)
	}
	return nil
}

type validatingBridgeHandoffService struct{ inner bridgeHandoffService }

func (s validatingBridgeHandoffService) Start(ctx context.Context, uid uint32, req apischema.NetworkBridgeHandoffRequest) (apischema.NetworkBridgeHandoffStatus, error) {
	if err := validateHandoffStartRequest(req); err != nil {
		return apischema.NetworkBridgeHandoffStatus{}, err
	}
	return s.inner.Start(ctx, uid, req)
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
