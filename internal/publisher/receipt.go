package publisher

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (v *Verified) receipt() *Receipt {
	publisherID := sha256.Sum256([]byte(v.bundleSHA256))

	operations := make([]OperationReceipt, len(v.operations))
	for index, operation := range v.operations {
		operations[index] = OperationReceipt{RequestID: operation.RequestID, Kind: operation.Kind}
	}

	return &Receipt{
		Version: 1, PublisherRunID: "publish-" + hex.EncodeToString(publisherID[:8]),
		BundleSHA256: v.bundleSHA256, PlanSHA256: v.planSHA256, PolicySHA256: v.policySHA256,
		RepositoryID: v.repositoryID, OperationSet: v.operationSet, Operations: operations,
	}
}

func (r *Receipt) JSON() ([]byte, error) {
	if r == nil || r.Version != 1 {
		return nil, ErrRejected
	}

	encoded, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encoding publisher receipt: %w", err)
	}

	return append(encoded, '\n'), nil
}

func (r *Receipt) Text() ([]byte, error) {
	if r == nil || r.Version != 1 {
		return nil, ErrRejected
	}

	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding publisher receipt: %w", err)
	}

	return append(encoded, '\n'), nil
}

func WriteReceipt(path string, receipt *Receipt) error {
	if path == "" || receipt == nil {
		return ErrRejected
	}

	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ErrRejected
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking publisher receipt: %w", err)
	}

	encoded, err := receipt.JSON()
	if err != nil {
		return err
	}

	parent := filepath.Dir(path)

	mkdirErr := os.MkdirAll(parent, 0o700)
	if mkdirErr != nil {
		return fmt.Errorf("creating publisher receipt directory: %w", mkdirErr)
	}

	temporary, err := os.CreateTemp(parent, ".duto-publisher-receipt-")
	if err != nil {
		return fmt.Errorf("creating publisher receipt: %w", err)
	}

	name := temporary.Name()
	defer os.Remove(name) //nolint:errcheck // temporary receipt cleanup is best effort

	chmodErr := temporary.Chmod(0o600)
	if chmodErr != nil {
		_ = temporary.Close()
		return fmt.Errorf("setting publisher receipt mode: %w", chmodErr)
	}

	_, writeErr := temporary.Write(encoded)
	if writeErr != nil {
		_ = temporary.Close()
		return fmt.Errorf("writing publisher receipt: %w", writeErr)
	}

	syncErr := temporary.Sync()
	if syncErr != nil {
		_ = temporary.Close()
		return fmt.Errorf("syncing publisher receipt: %w", syncErr)
	}

	if closeErr := temporary.Close(); closeErr != nil {
		return fmt.Errorf("closing publisher receipt: %w", closeErr)
	}

	if renameErr := os.Rename(name, path); renameErr != nil {
		return fmt.Errorf("renaming publisher receipt: %w", renameErr)
	}

	return nil
}
