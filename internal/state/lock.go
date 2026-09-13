package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
)

// Lock roles: the archive root holds the owning lock; the video archive root holds a mirror lock
// naming the archive run.
const (
	RoleArchive = "archive"
	RoleMirror  = "mirror"
)

// LockInfo is the JSON content of root/.arxgo/lock.
type LockInfo struct {
	V         int       `json:"v"`
	RunID     string    `json:"run_id"`
	PID       int       `json:"pid"`
	Host      string    `json:"host"`
	StartedAt time.Time `json:"started_at"`
	Op        string    `json:"op"`
	Role      string    `json:"role"`
	Root      string    `json:"root"`
	Peer      string    `json:"peer,omitempty"`
}

func (l LockInfo) sameOwner(o LockInfo) bool {
	return l.RunID == o.RunID && l.PID == o.PID && l.Host == o.Host && l.StartedAt.Equal(o.StartedAt)
}

// LockState classifies a lock found on disk.
type LockState string

// Lock states reported by LockedError.
const (
	LockHeld       LockState = "held"       // the owner process is alive on this host
	LockStale      LockState = "stale"      // the owner process on this host is gone
	LockRemote     LockState = "remote"     // owned by another host; never taken over
	LockUnreadable LockState = "unreadable" // empty or torn lock file
)

// ErrLocked matches every *LockedError.
var ErrLocked = errors.New("run lock not acquired")

// ErrLockLost reports that a lock this process held was removed or replaced by someone else.
var ErrLockLost = errors.New("run lock lost")

// LockedError reports a lock that this process may not take.
type LockedError struct {
	Path  string
	State LockState
	Owner LockInfo // zero for LockUnreadable
	Err   error    // decode error for LockUnreadable
}

func (e *LockedError) Error() string {
	o := e.Owner
	owner := fmt.Sprintf("run %s (op %s, pid %d on host %s, started %s)",
		o.RunID, o.Op, o.PID, o.Host, o.StartedAt.UTC().Format(time.RFC3339))
	switch e.State {
	case LockStale:
		return fmt.Sprintf("run lock %s is stale: %s is no longer running; "+
			"make sure no arxgo uses this root, then rerun with --force-unlock", e.Path, owner)
	case LockRemote:
		return fmt.Sprintf("run lock %s is held by %s on another host; it is never taken over "+
			"automatically: stop that run, or delete the file once it is known to be stopped", e.Path, owner)
	case LockUnreadable:
		return fmt.Sprintf("run lock %s is unreadable (%v); "+
			"if no arxgo is running, rerun with --force-unlock", e.Path, e.Err)
	default:
		return fmt.Sprintf("run lock %s is held by %s", e.Path, owner)
	}
}

// Unwrap lets errors.Is(err, ErrLocked) match.
func (e *LockedError) Unwrap() error { return ErrLocked }

// LockOptions describes the acquiring process. Zero Host, PID and Alive use the real system.
type LockOptions struct {
	Host        string
	PID         int
	Alive       func(pid int) bool
	ForceUnlock bool
}

func (o LockOptions) withDefaults() LockOptions {
	if o.Host == "" {
		o.Host = Hostname()
	}
	if o.PID == 0 {
		o.PID = os.Getpid()
	}
	if o.Alive == nil {
		o.Alive = fsops.ProcessAlive
	}
	return o
}

// Hostname returns the host name recorded in locks, or "unknown" when it cannot be read.
func Hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}

// Lock is a run lock held by this process.
type Lock struct {
	path string
	info LockInfo
	// TookOver is the stale or unreadable lock replaced under ForceUnlock, for logging.
	TookOver *LockInfo
}

// Path returns the lock file path.
func (l *Lock) Path() string { return l.path }

// Info returns the lock content written by this process.
func (l *Lock) Info() LockInfo { return l.info }

// LockPath returns root/.arxgo/lock.
func LockPath(root string) string { return filepath.Join(StateDir(root), lockName) }

// unreadableRetries bounds re-reads of a lock that does not decode, which may be one another
// process has created but not yet written.
var unreadableRetries = []time.Duration{20 * time.Millisecond, 80 * time.Millisecond}

// AcquireLock creates root/.arxgo/lock with O_CREATE|O_EXCL. info supplies the run id, start time,
// operation, role and peer; version, pid, host and root are filled in. An existing lock yields a
// *LockedError unless it is stale or unreadable and opts.ForceUnlock is set; locks of another host
// are never taken over.
func AcquireLock(root string, info LockInfo, opts LockOptions) (*Lock, error) {
	opts = opts.withDefaults()
	if err := mkdirDurable(StateDir(root)); err != nil {
		return nil, err
	}
	path := LockPath(root)
	info.V, info.PID, info.Host, info.Root = 1, opts.PID, opts.Host, root
	lock := &Lock{path: path, info: info}
	for attempt := 0; attempt < 5; attempt++ {
		err := createLockFile(path, info)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		owner, raw, lerr := inspectLock(path, opts)
		if errors.Is(lerr, fs.ErrNotExist) {
			continue // released between our create and read
		}
		var locked *LockedError
		if !errors.As(lerr, &locked) {
			return nil, lerr
		}
		if locked.State == LockHeld || locked.State == LockRemote || !opts.ForceUnlock {
			return nil, locked
		}
		switch err := takeOver(path, raw); {
		case errors.Is(err, errLockChanged):
			continue
		case err != nil:
			return nil, err
		}
		prev := owner
		lock.TookOver = &prev
	}
	return nil, fmt.Errorf("run lock %s: repeatedly replaced by other processes; retry", path)
}

func createLockFile(path string, info LockInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return err
	}
	if err := fsops.SyncDir(filepath.Dir(path)); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

// ReadLock reads and decodes a lock file.
func ReadLock(path string) (LockInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return LockInfo{}, err
	}
	var info LockInfo
	if err := decodeLock(raw, &info); err != nil {
		return LockInfo{}, fmt.Errorf("run lock %s: %w", path, err)
	}
	return info, nil
}

// Verify checks that the lock file still belongs to this process.
func (l *Lock) Verify() error {
	cur, err := ReadLock(l.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %s was removed", ErrLockLost, l.path)
	case err != nil:
		return fmt.Errorf("%w: %v", ErrLockLost, err)
	case !cur.sameOwner(l.info):
		return fmt.Errorf("%w: %s now belongs to run %s (pid %d on host %s)", ErrLockLost, l.path, cur.RunID, cur.PID, cur.Host)
	}
	return nil
}

// SetRunID rewrites the lock with another run id, used when the process resumes an earlier run.
func (l *Lock) SetRunID(id string) error {
	if err := l.Verify(); err != nil {
		return err
	}
	info := l.info
	info.RunID = id
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	if err := fsops.AtomicWriteFile(l.path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	l.info = info
	return nil
}

// Release removes the lock after verifying that it still belongs to this process. A lock that was
// taken over is left in place and ErrLockLost is returned.
func (l *Lock) Release() error {
	if err := l.Verify(); err != nil {
		return err
	}
	if err := os.Remove(l.path); err != nil {
		return err
	}
	return fsops.SyncDir(filepath.Dir(l.path))
}
