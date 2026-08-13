package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const standaloneUpdateJournalVersion = 1

var defaultStandaloneUpdateJournal = standaloneUpdateJournal{
	path: filepath.Join(version.DataDir, "docker-update-transaction.json"),
}

type standaloneUpdatePhase string

const (
	standaloneUpdatePrepared standaloneUpdatePhase = "prepared"
	standaloneUpdateCreated  standaloneUpdatePhase = "created"
	standaloneUpdateVerified standaloneUpdatePhase = "verified"
)

type standaloneUpdateTransaction struct {
	Version       int                   `json:"version"`
	Phase         standaloneUpdatePhase `json:"phase"`
	OriginalID    string                `json:"original_id"`
	OriginalName  string                `json:"original_name"`
	BackupName    string                `json:"backup_name"`
	ReplacementID string                `json:"replacement_id,omitempty"`
}

type standaloneUpdateJournal struct {
	path string
}

func (j standaloneUpdateJournal) write(tx standaloneUpdateTransaction) error {
	tx.Version = standaloneUpdateJournalVersion
	data, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal standalone update transaction: %w", err)
	}
	if err := utils.WriteFileAtomic(j.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write standalone update transaction: %w", err)
	}
	return nil
}

func (j standaloneUpdateJournal) read() (standaloneUpdateTransaction, bool, error) {
	data, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return standaloneUpdateTransaction{}, false, nil
	}
	if err != nil {
		return standaloneUpdateTransaction{}, false, fmt.Errorf("read standalone update transaction: %w", err)
	}
	var tx standaloneUpdateTransaction
	if err := json.Unmarshal(data, &tx); err != nil {
		return standaloneUpdateTransaction{}, false, fmt.Errorf("parse standalone update transaction: %w", err)
	}
	if tx.Version != standaloneUpdateJournalVersion {
		return standaloneUpdateTransaction{}, false, fmt.Errorf("unsupported standalone update transaction version %d", tx.Version)
	}
	if tx.OriginalID == "" || tx.OriginalName == "" || tx.BackupName == "" {
		return standaloneUpdateTransaction{}, false, errors.New("standalone update transaction is incomplete")
	}
	return tx, true, nil
}

func (j standaloneUpdateJournal) clear() error {
	if err := os.Remove(j.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear standalone update transaction: %w", err)
	}
	return nil
}

func recoverStandaloneUpdate(ctx context.Context, cli nativeContainerUpdateClient, journal standaloneUpdateJournal) error {
	tx, ok, err := journal.read()
	if err != nil || !ok {
		return err
	}

	originalResult, originalErr := cli.ContainerInspect(ctx, tx.OriginalID, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(originalErr) {
		if tx.Phase == standaloneUpdateVerified {
			return journal.clear()
		}
		return fmt.Errorf("recover standalone update for %q: original container is missing before verification", tx.OriginalName)
	}
	if originalErr != nil {
		return fmt.Errorf("inspect journaled rollback container %q: %w", tx.OriginalID, originalErr)
	}
	original := originalResult.Container
	originalName := strings.TrimPrefix(original.Name, "/")

	switch tx.Phase {
	case standaloneUpdatePrepared:
		return recoverPreparedStandaloneUpdate(ctx, cli, journal, tx, originalName, original.State != nil && original.State.Running)
	case standaloneUpdateCreated:
		return recoverCreatedStandaloneUpdate(ctx, cli, journal, tx, originalName)
	case standaloneUpdateVerified:
		return recoverVerifiedStandaloneUpdate(ctx, cli, journal, tx, original)
	default:
		return fmt.Errorf("unsupported standalone update transaction phase %q", tx.Phase)
	}
}

func recoverPreparedStandaloneUpdate(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	journal standaloneUpdateJournal,
	tx standaloneUpdateTransaction,
	originalName string,
	originalRunning bool,
) error {
	if originalName == tx.OriginalName {
		if !originalRunning {
			if err := startOriginalContainer(ctx, cli, tx.OriginalID); err != nil {
				return err
			}
		}
		return journal.clear()
	}
	if originalName != tx.BackupName {
		return fmt.Errorf("journaled rollback container %q has unexpected name %q", tx.OriginalID, originalName)
	}
	replacementResult, replacementErr := cli.ContainerInspect(ctx, tx.OriginalName, client.ContainerInspectOptions{})
	if replacementErr == nil {
		if replacementResult.Container.ID == tx.OriginalID {
			return fmt.Errorf("journaled rollback container %q unexpectedly owns name %q", tx.OriginalID, tx.OriginalName)
		}
		if _, err := cli.ContainerRemove(ctx, replacementResult.Container.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
			return fmt.Errorf("remove unverified replacement container %q: %w", replacementResult.Container.ID, err)
		}
	} else if !errdefs.IsNotFound(replacementErr) {
		return fmt.Errorf("inspect possible replacement container %q: %w", tx.OriginalName, replacementErr)
	}
	if err := restoreOriginalContainer(ctx, cli, tx.OriginalID, tx.OriginalName); err != nil {
		return err
	}
	return journal.clear()
}

func recoverCreatedStandaloneUpdate(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	journal standaloneUpdateJournal,
	tx standaloneUpdateTransaction,
	originalName string,
) error {
	if tx.ReplacementID == "" || originalName != tx.BackupName {
		return fmt.Errorf("journaled standalone update for %q is incomplete", tx.OriginalName)
	}
	if err := rollbackStandaloneContainer(ctx, cli, tx.ReplacementID, tx.OriginalID, tx.OriginalName); err != nil {
		return err
	}
	return journal.clear()
}

func recoverVerifiedStandaloneUpdate(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	journal standaloneUpdateJournal,
	tx standaloneUpdateTransaction,
	original container.InspectResponse,
) error {
	originalName := strings.TrimPrefix(original.Name, "/")
	if tx.ReplacementID == "" || originalName != tx.BackupName || original.State == nil || original.State.Running {
		return fmt.Errorf("journaled rollback container %q does not match a verified update", tx.OriginalID)
	}
	replacementResult, err := cli.ContainerInspect(ctx, tx.ReplacementID, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect journaled replacement container %q: %w", tx.ReplacementID, err)
	}
	replacement := replacementResult.Container
	if strings.TrimPrefix(replacement.Name, "/") != tx.OriginalName || replacement.State == nil || !replacement.State.Running {
		return fmt.Errorf("journaled replacement container %q is not active under name %q", tx.ReplacementID, tx.OriginalName)
	}
	if _, err := cli.ContainerRemove(ctx, tx.OriginalID, client.ContainerRemoveOptions{}); err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("remove journaled rollback container %q: %w", tx.BackupName, err)
	}
	return journal.clear()
}
