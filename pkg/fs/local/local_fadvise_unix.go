// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build linux || freebsd || netbsd || aix

package local

import (
	"os"

	"golang.org/x/sys/unix"
)

// fadviseSequential hints the kernel that the file will be read sequentially.
func fadviseSequential(f *os.File) error {
	return fadvise(f, unix.FADV_SEQUENTIAL)
}

// fadviseDontNeed hints the kernel that cached pages for this file can be dropped
// after the application is done reading (reduces page cache retention during migration).
func fadviseDontNeed(f *os.File) error {
	return fadvise(f, unix.FADV_DONTNEED)
}

func fadvise(f *os.File, advice int) error {
	if f == nil {
		return nil
	}
	raw, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var fadviseErr error
	err = raw.Control(func(fd uintptr) {
		fadviseErr = unix.Fadvise(int(fd), 0, 0, advice)
	})
	if err != nil {
		return err
	}
	// EINVAL/ESPIPE: kernel or fd type may not support advice; treat as non-fatal
	if fadviseErr == unix.EINVAL || fadviseErr == unix.ESPIPE {
		return nil
	}
	return fadviseErr
}
