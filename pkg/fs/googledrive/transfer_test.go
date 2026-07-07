// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package googledrive

import (
	"io"
	"testing"
)

func TestParsePendingFileID(t *testing.T) {
	parent, name, ok := parsePendingFileID(pendingFileID("parent123", "report.pdf"))
	if !ok || parent != "parent123" || name != "report.pdf" {
		t.Fatalf("got parent=%q name=%q ok=%v", parent, name, ok)
	}
	if _, _, ok := parsePendingFileID("real-file-id"); ok {
		t.Fatal("expected non-pending id to fail parse")
	}
}

func TestPendingFileIDRoundTrip(t *testing.T) {
	id := pendingFileID("abc", "my:file:name")
	parent, name, ok := parsePendingFileID(id)
	if !ok || parent != "abc" || name != "my:file:name" {
		t.Fatalf("got parent=%q name=%q ok=%v", parent, name, ok)
	}
}

func TestDriveWriterStreamsChunksWithoutBufferingEntireFile(t *testing.T) {
	d := &DriveFS{session: &Session{}}
	w, err := newDriveWriter(d, t.Context(), pendingFileID("parent", "chunk.bin"))
	if err != nil {
		t.Fatal(err)
	}

	received := make(chan []byte, 1)
	testStreamUploadHook = func(_ *driveWriter, body io.Reader) error {
		b, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		received <- b
		return nil
	}
	t.Cleanup(func() { testStreamUploadHook = nil })

	if _, err := w.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("def")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	got := <-received
	if string(got) != "abcdef" {
		t.Fatalf("upload body=%q want abcdef", got)
	}
}
