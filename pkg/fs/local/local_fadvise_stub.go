// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build !(linux || freebsd || netbsd || aix)

package local

import "os"

func fadviseSequential(f *os.File) error {
	_ = f
	return nil
}

func fadviseDontNeed(f *os.File) error {
	_ = f
	return nil
}
