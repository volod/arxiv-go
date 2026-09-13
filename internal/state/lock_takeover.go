package state

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
)

// inspectLock reads an existing lock and classifies it. It returns the raw bytes so a takeover can
// check that it removes exactly the lock it judged.
func inspectLock(path string, opts LockOptions) (LockInfo, []byte, error) {
	var decodeErr error
	for i := 0; ; i++ {
		raw, err := os.ReadFile(path)
		if err != nil {
			return LockInfo{}, nil, err
		}
		var owner LockInfo
		if decodeErr = decodeLock(raw, &owner); decodeErr == nil {
			return owner, raw, &LockedError{Path: path, State: classify(owner, opts), Owner: owner}
		}
		if i == len(unreadableRetries) {
			return LockInfo{}, raw, &LockedError{Path: path, State: LockUnreadable, Err: decodeErr}
		}
		time.Sleep(unreadableRetries[i])
	}
}

func decodeLock(raw []byte, info *LockInfo) error {
	if err := json.Unmarshal(raw, info); err != nil {
		return err
	}
	if info.V != 1 || info.RunID == "" || info.PID <= 0 || info.Host == "" {
		return fmt.Errorf("unsupported lock content (v=%d)", info.V)
	}
	return nil
}

func classify(owner LockInfo, opts LockOptions) LockState {
	if !strings.EqualFold(owner.Host, opts.Host) {
		return LockRemote
	}
	// A lock naming our own pid was left by an earlier process whose pid was reused (for example
	// pid 1 in a container): this process has not taken it.
	if owner.PID != opts.PID && opts.Alive(owner.PID) {
		return LockHeld
	}
	return LockStale
}

var errLockChanged = errors.New("lock changed during takeover")

// takeOver moves the judged lock aside under a unique name and removes it. If another process
// replaced the lock after it was read, the new lock is put back and errLockChanged is returned.
func takeOver(path string, judged []byte) error {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return err
	}
	aside := path + "." + hex.EncodeToString(suffix[:]) + fsops.PartSuffix
	if err := os.Rename(path, aside); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return errLockChanged
		}
		return err
	}
	got, err := os.ReadFile(aside)
	if err != nil || !bytes.Equal(got, judged) {
		if rerr := fsops.Rename(aside, path); rerr != nil {
			if errors.Is(rerr, fs.ErrExist) {
				os.Remove(aside)
				return errLockChanged
			}
			var le *os.LinkError
			if errors.As(rerr, &le) {
				return fmt.Errorf("run lock %s changed during takeover and could not be restored from %s: %w", path, aside, rerr)
			}
		}
		return errLockChanged
	}
	if err := os.Remove(aside); err != nil {
		return err
	}
	return fsops.SyncDir(filepath.Dir(path))
}
