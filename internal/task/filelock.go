package task

import "sync"

// FileLockService prevents two agents from modifying the same file concurrently.
// It tracks which files are locked by which subtask, and allows the orchestrator
// to check whether a subtask's files are available before spawning an agent.
type FileLockService struct {
	mu    sync.Mutex
	locks map[string]string // filePath -> subtaskID that holds the lock
}

// NewFileLockService creates a new FileLockService.
func NewFileLockService() *FileLockService {
	return &FileLockService{
		locks: make(map[string]string),
	}
}

// TryLock attempts to acquire locks on all the given files for a subtask.
// Returns true if all files were locked successfully.
// Returns false if any file is already locked by another subtask (no locks are acquired).
func (fls *FileLockService) TryLock(subtaskID string, files []string) bool {
	if len(files) == 0 {
		return true
	}

	fls.mu.Lock()
	defer fls.mu.Unlock()

	// Check if any file is already locked by a different subtask
	for _, f := range files {
		if holder, ok := fls.locks[f]; ok && holder != subtaskID {
			return false
		}
	}

	// All clear — acquire locks
	for _, f := range files {
		fls.locks[f] = subtaskID
	}
	return true
}

// Release releases all file locks held by the given subtask.
func (fls *FileLockService) Release(subtaskID string) {
	fls.mu.Lock()
	defer fls.mu.Unlock()

	for f, holder := range fls.locks {
		if holder == subtaskID {
			delete(fls.locks, f)
		}
	}
}

// LockedFiles returns the list of files currently locked, with their holders.
func (fls *FileLockService) LockedFiles() map[string]string {
	fls.mu.Lock()
	defer fls.mu.Unlock()

	cp := make(map[string]string, len(fls.locks))
	for f, h := range fls.locks {
		cp[f] = h
	}
	return cp
}
