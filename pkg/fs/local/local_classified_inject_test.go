// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestListChildrenInjectedEIOAmbiguousPromotion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.clearInject)

	l.SetActiveWorkers(8)
	now := time.Now()
	tr := l.degradation.AmbiguousTracker()
	for i := 0; i < 4; i++ {
		tr.Record("ListChildren", "EIO", 8, now.Add(time.Duration(i)*10*time.Millisecond))
	}

	l.InjectBeforeOp(func(operation string, attempt int) error {
		if operation != "ListChildren" {
			return nil
		}
		if attempt < 2 {
			return fmt.Errorf("read dir: %w", syscall.EIO)
		}
		return nil
	})

	_, err = l.ListChildren(context.Background(), dir, nil, "/")
	if err != nil {
		t.Fatal(err)
	}
	if l.degradation.TakeRecentHits() == 0 {
		t.Fatal("expected degradation hits after suspected throttle promotion")
	}
}

func TestListChildrenInjectedFatalNoRetry(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.clearInject)

	var calls int
	l.InjectBeforeOp(func(operation string, attempt int) error {
		if operation == "ListChildren" {
			calls++
			return syscall.EACCES
		}
		return nil
	})

	_, err = l.ListChildren(context.Background(), dir, nil, "/")
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls=%d want 1 (fatal should not retry)", calls)
	}
}

func TestCreateFolderClassifiedRetryTransient(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.clearInject)

	var calls int
	l.InjectBeforeOp(func(operation string, attempt int) error {
		if operation != "CreateFolder" {
			return nil
		}
		calls++
		if calls == 1 {
			return fmt.Errorf("mkdir: %w", syscall.EIO)
		}
		return nil
	})

	sub := filepath.Join(dir, "sub")
	_, err = l.CreateFolder(context.Background(), dir, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sub); err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatalf("calls=%d want >=2", calls)
	}
}

func TestOpenWriteUsesClassifiedRetry(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "w.txt")
	if err := os.WriteFile(p, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.clearInject)

	var calls int
	l.InjectBeforeOp(func(operation string, attempt int) error {
		if operation != "OpenWrite" {
			return nil
		}
		calls++
		if calls == 1 {
			return syscall.EAGAIN
		}
		return nil
	})

	wc, err := l.OpenWrite(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatalf("calls=%d want >=2", calls)
	}
}

func TestDegradationReporterInterface(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	var _ types.FSDegradationReporter = l
}
