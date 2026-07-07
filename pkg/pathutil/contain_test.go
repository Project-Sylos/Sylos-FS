// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWithinRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		testWithinRootWindows(t)
		return
	}
	testWithinRootUnix(t)
}

func testWithinRootUnix(t *testing.T) {
	t.Helper()
	cases := []struct {
		root   string
		target string
		want   bool
	}{
		{root: "/", target: "/home/user/file", want: true},
		{root: "/", target: "/", want: true},
		{root: "/home/data", target: "/home/data/nested/file", want: true},
		{root: "/home/data", target: "/home/data", want: true},
		{root: "/home/data", target: "/etc/passwd", want: false},
		{root: "/home/data", target: "/home/data-other", want: false},
	}
	for _, tc := range cases {
		got, err := WithinRoot(tc.root, tc.target)
		if err != nil {
			t.Fatalf("WithinRoot(%q, %q): %v", tc.root, tc.target, err)
		}
		if got != tc.want {
			t.Fatalf("WithinRoot(%q, %q) = %v, want %v", tc.root, tc.target, got, tc.want)
		}
	}
}

func testWithinRootWindows(t *testing.T) {
	t.Helper()
	root := `C:\data`
	target := filepath.Join(`C:\data`, "nested", "file.txt")
	got, err := WithinRoot(root, target)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatalf("expected %q under %q", target, root)
	}

	outside := `C:\other\file.txt`
	got, err = WithinRoot(root, outside)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatalf("expected %q outside %q", outside, root)
	}
}

func TestWithinRootTempDir(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	ok, err := WithinRoot(root, nested)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected nested dir under temp root")
	}

	sibling := filepath.Join(filepath.Dir(root), "outside-"+filepath.Base(root))
	ok, err = WithinRoot(root, sibling)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("expected sibling path outside temp root")
	}
}
