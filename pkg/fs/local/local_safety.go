// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// ErrNotRegularFile is returned by OpenRead/OpenWrite when the path is not a regular file
// (e.g. directory, FIFO, socket, device). The engine can use errors.Is to classify.
var ErrNotRegularFile = errors.New("localfs: not a regular file")

// MaxRegularFileSize is the maximum size (bytes) we accept for listing as a copyable file.
// Pseudo files often report absurd sizes; above this we skip.
const MaxRegularFileSize int64 = 1 << 40 // 1 TiB

var pseudoFSPrefixes = []string{
	"/proc", "/sys", "/dev", "/run",
}

// isPseudoFSPath returns true if path is exactly one of the pseudo roots or under them.
func isPseudoFSPath(path string) bool {
	p := strings.ReplaceAll(path, "\\", "/")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return false
	}
	for _, prefix := range pseudoFSPrefixes {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
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
	onWarning("localfs: virtual/pseudo filesystem detected under root (e.g. /proc, /sys); traversal may skip many entries")
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
