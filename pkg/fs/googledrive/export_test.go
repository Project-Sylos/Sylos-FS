// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package googledrive

import (
	"testing"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
)

func TestEnsureExtension(t *testing.T) {
	tests := []struct {
		name string
		ext  string
		want string
	}{
		{"Agave Writing Exercise", ".docx", "Agave Writing Exercise.docx"},
		{"Already.docx", ".docx", "Already.docx"},
		{"Sheet", ".xlsx", "Sheet.xlsx"},
	}
	for _, tc := range tests {
		if got := ensureExtension(tc.name, tc.ext); got != tc.want {
			t.Fatalf("ensureExtension(%q,%q)=%q want %q", tc.name, tc.ext, got, tc.want)
		}
	}
}

func TestExportSpecForMIME(t *testing.T) {
	spec, ok := exportSpecForMIME(mimeGoogleDocument)
	if !ok || spec.Ext != ".docx" {
		t.Fatalf("document spec: ok=%v spec=%+v", ok, spec)
	}
	form, ok := exportSpecForMIME(mimeGoogleForm)
	if !ok || form.MIME != "application/zip" || form.Ext != ".zip" {
		t.Fatalf("form spec: ok=%v spec=%+v", ok, form)
	}
	_, ok = exportSpecForMIME(mimeGoogleMap)
	if ok {
		t.Fatal("maps should not be exportable")
	}
}

func TestStripMisleadingExtension(t *testing.T) {
	if got := stripMisleadingExtension("Estate Sale Postings.pdf", ".zip"); got != "Estate Sale Postings" {
		t.Fatalf("got %q", got)
	}
	if got := displayNameForExport("Estate Sale Postings.pdf", mimeGoogleForm); got != "Estate Sale Postings.zip" {
		t.Fatalf("form display name: got %q", got)
	}
	if got := displayNameForExport("Real Report.pdf", "application/pdf"); got != "Real Report.pdf" {
		t.Fatalf("binary name unchanged: got %q", got)
	}
}

func TestIsExportConversionUnsupported(t *testing.T) {
	err := &googleapi.Error{
		Code:    400,
		Message: "The requested conversion is not supported.",
	}
	if !isExportConversionUnsupported(err) {
		t.Fatal("expected unsupported conversion")
	}
}

func TestListableDriveFile(t *testing.T) {
	doc := &drive.File{MimeType: mimeGoogleDocument, Name: "doc"}
	if !listableDriveFile(doc) {
		t.Fatal("google doc should be listable")
	}
	site := &drive.File{MimeType: mimeGoogleSite, Name: "site"}
	if listableDriveFile(site) {
		t.Fatal("google site should not be listable")
	}
	pdf := &drive.File{MimeType: "application/pdf", Name: "file.pdf"}
	if !listableDriveFile(pdf) {
		t.Fatal("binary pdf should be listable")
	}
	shortcut := &drive.File{
		MimeType: mimeGoogleShortcut,
		Name:     "link",
		ShortcutDetails: &drive.FileShortcutDetails{
			TargetId:       "target",
			TargetMimeType: mimeGoogleDocument,
		},
	}
	if !listableDriveFile(shortcut) {
		t.Fatal("shortcut to doc should be listable")
	}
}

func TestEffectiveContentMIME(t *testing.T) {
	shortcut := &drive.File{
		MimeType: mimeGoogleShortcut,
		ShortcutDetails: &drive.FileShortcutDetails{
			TargetMimeType: mimeGoogleSheet,
		},
	}
	if got := effectiveContentMIME(shortcut); got != mimeGoogleSheet {
		t.Fatalf("got %q", got)
	}
}
