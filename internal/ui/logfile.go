package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OpenLog creates update-YYYYMMDD-HHMMSS.log under dir and removes the
// oldest update logs so that at most keep remain, the new one included.
func OpenLog(dir string, now time.Time, keep int) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	name := "update-" + now.Format("20060102-150405") + ".log"

	f, err := AppendLog(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}

	// Pruning is housekeeping: a failure keeps old logs but never costs this
	// run its log.
	pruneLogs(dir, name, keep)

	return f, nil
}

// AppendLog opens path for appending, creating it private when missing.
func AppendLog(path string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}

	return f, nil
}

// pruneLogs never removes current, so a clock that went backwards cannot
// delete the log just opened.
func pruneLogs(dir, current string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var old []string

	for _, e := range entries {
		n := e.Name()
		if e.Type().IsRegular() && n != current && strings.HasPrefix(n, "update-") && strings.HasSuffix(n, ".log") {
			old = append(old, n)
		}
	}

	// ReadDir sorts by name, and the timestamped names sort oldest first.
	for _, n := range old[:max(len(old)-max(keep-1, 0), 0)] {
		_ = os.Remove(filepath.Join(dir, n))
	}
}
