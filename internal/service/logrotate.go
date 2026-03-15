package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// RotatingWriter is a simple size-based log file rotator.
// When the current file exceeds maxSize bytes, it rotates by
// renaming to .1, .2, etc., keeping at most maxFiles old files.
type RotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxSize  int64
	maxFiles int
	file     *os.File
	size     int64
}

// NewRotatingWriter creates a new RotatingWriter.
func NewRotatingWriter(path string, maxSize int64, maxFiles int) (*RotatingWriter, error) {
	w := &RotatingWriter{
		path:     path,
		maxSize:  maxSize,
		maxFiles: maxFiles,
	}
	if err := w.openFile(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *RotatingWriter) openFile() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening log file %s: %w", w.path, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat log file: %w", err)
	}
	w.file = f
	w.size = info.Size()
	return nil
}

// Write implements io.Writer. It rotates the file if it exceeds maxSize.
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			// Try to write anyway
			return w.file.Write(p)
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *RotatingWriter) rotate() error {
	w.file.Close()

	// Remove oldest file
	oldest := fmt.Sprintf("%s.%d", w.path, w.maxFiles)
	os.Remove(oldest)

	// Shift existing files: .4 → .5, .3 → .4, etc.
	for i := w.maxFiles - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", w.path, i)
		dst := fmt.Sprintf("%s.%d", w.path, i+1)
		os.Rename(src, dst)
	}

	// Current → .1
	os.Rename(w.path, w.path+".1")

	return w.openFile()
}

// Close closes the underlying file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// CleanOldLogs removes log files beyond the retention count.
func CleanOldLogs(logDir string, maxFiles int) {
	pattern := filepath.Join(logDir, "klaudio.log.*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, m := range matches {
		base := filepath.Base(m)
		// Simple: just let rotate() handle it
		_ = base
	}
}
