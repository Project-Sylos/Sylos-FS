// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// ErrNotRegularFile is returned by OpenRead/OpenWrite when the path is not a regular file
// (e.g. directory, FIFO, socket, device). The engine can use errors.Is to classify.
var ErrNotRegularFile = errors.New("localfs: not a regular file")

// MaxRegularFileSize is the maximum size (bytes) we accept for listing as a copyable file.
// Pseudo files often report absurd sizes; above this we skip.
const MaxRegularFileSize int64 = 1 << 40 // 1 TiB

// readDirBatchSize is how many dirents we pull per ReadDir call so ListChildren can
// honor context cancellation between batches (avoids unbounded hangs on huge dirs).
const readDirBatchSize = 256

// normalizeFSPath cleans path separators for prefix matching.
func normalizeFSPath(path string) string {
	p := strings.ReplaceAll(path, "\\", "/")
	if p == "" {
		return p
	}
	cleaned := filepath.Clean(p)
	cleaned = strings.ReplaceAll(cleaned, "\\", "/")
	return strings.TrimSuffix(cleaned, "/")
}

// isBlockedPath reports whether path is under a hard-denied prefix (proc/sys/dev/run,
// Windows device namespaces, etc.). These must never be listed or opened during migration.
func isBlockedPath(path string) bool {
	p := normalizeFSPath(path)
	if p == "" {
		return false
	}
	lower := strings.ToLower(p)
	for _, prefix := range blockedPathPrefixes {
		pref := strings.TrimSuffix(strings.ReplaceAll(prefix, "\\", "/"), "/")
		prefLower := strings.ToLower(pref)
		if lower == prefLower || strings.HasPrefix(lower, prefLower+"/") {
			return true
		}
	}
	return false
}

// errBlockedPath wraps types.ErrPathBlocked with the offending path.
func errBlockedPath(path string) error {
	return fmt.Errorf("%w: %s", types.ErrPathBlocked, path)
}

// listableDirInfo returns file info if identifier can be safely passed to ReadDir
// (must be a directory). Symlinks to directories are followed once via Stat.
func listableDirInfo(identifier string) (os.FileInfo, error) {
	fi, err := os.Lstat(identifier)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		fi, err = os.Stat(identifier)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("localfs: not a directory: %s", identifier)
		}
		return fi, nil
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("localfs: not a directory: %s", identifier)
	}
	return fi, nil
}

// childListableAsFolder returns true if fi represents a directory (traversable).
func childListableAsFolder(fi os.FileInfo) bool {
	return fi.Mode().IsDir()
}

// childCopyableAsFile returns true if fi is a regular file with sane size.
func childCopyableAsFile(fi os.FileInfo) bool {
	if !fi.Mode().IsRegular() {
		return false
	}
	sz := fi.Size()
	if sz < 0 || sz > MaxRegularFileSize {
		return false
	}
	return true
}

// warnState holds once-per-adapter warning flags.
type warnState struct {
	mu              sync.Mutex
	pseudoFSWarned  bool
	fsBoundaryPaths map[string]struct{} // warn once per path when device changes
}

func (w *warnState) warnPseudoFS(onWarning func(string)) {
	if onWarning == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pseudoFSWarned {
		return
	}
	w.pseudoFSWarned = true
	onWarning("localfs: migration root is under a blocked path (e.g. /proc, /sys); listing will be refused")
}

func (w *warnState) warnBlockedChild(onWarning func(string), path string) {
	if onWarning == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fsBoundaryPaths == nil {
		w.fsBoundaryPaths = make(map[string]struct{})
	}
	key := "blocked:" + path
	if _, ok := w.fsBoundaryPaths[key]; ok {
		return
	}
	w.fsBoundaryPaths[key] = struct{}{}
	onWarning(fmt.Sprintf("localfs: skipping blocked path %s", path))
}

func (w *warnState) warnFsBoundary(onWarning func(string), path string) {
	if onWarning == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fsBoundaryPaths == nil {
		w.fsBoundaryPaths = make(map[string]struct{})
	}
	if _, ok := w.fsBoundaryPaths[path]; ok {
		return
	}
	w.fsBoundaryPaths[path] = struct{}{}
	onWarning(fmt.Sprintf("localfs: crossed filesystem boundary at %s", path))
}
