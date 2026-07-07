// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package googledrive

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"google.golang.org/api/drive/v3"
)

const googleAppsPrefix = "application/vnd.google-apps."

const (
	mimeGoogleFolder    = "application/vnd.google-apps.folder"
	mimeGoogleDocument  = "application/vnd.google-apps.document"
	mimeGoogleSheet     = "application/vnd.google-apps.spreadsheet"
	mimeGoogleSlides    = "application/vnd.google-apps.presentation"
	mimeGoogleDrawing   = "application/vnd.google-apps.drawing"
	mimeGoogleForm      = "application/vnd.google-apps.form"
	mimeGoogleScript    = "application/vnd.google-apps.script"
	mimeGoogleShortcut  = "application/vnd.google-apps.shortcut"
	mimeGoogleMap       = "application/vnd.google-apps.map"
	mimeGoogleSite      = "application/vnd.google-apps.site"
)

// exportSpec describes how a Google Workspace native file is exported for migration.
type exportSpec struct {
	MIME      string
	Ext       string
	Fallbacks []string // alternate export MIME types when the primary conversion is unsupported
}

var exportSpecs = map[string]exportSpec{
	mimeGoogleDocument: {
		MIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Ext:  ".docx",
	},
	mimeGoogleSheet: {
		MIME: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		Ext:  ".xlsx",
	},
	mimeGoogleSlides: {
		MIME: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		Ext:  ".pptx",
	},
	mimeGoogleDrawing: {
		MIME:      "application/pdf",
		Ext:       ".pdf",
		Fallbacks: []string{"image/png", "image/jpeg", "image/svg+xml"},
	},
	// Drive API only supports zip for forms (not PDF); see exportFormats in about.get.
	mimeGoogleForm: {
		MIME: "application/zip",
		Ext:  ".zip",
	},
	mimeGoogleScript: {MIME: "application/vnd.google-apps.script+json", Ext: ".json"},
}

func listChildrenFields() string {
	return "files(id,name,mimeType,size,modifiedTime,parents,shortcutDetails(targetId,targetMimeType))"
}

func exportSpecForMIME(mimeType string) (exportSpec, bool) {
	spec, ok := exportSpecs[mimeType]
	return spec, ok
}

// exportMIMEsForType returns primary and fallback export MIME types for a Workspace native type.
func exportMIMEsForType(mimeType string) []string {
	spec, ok := exportSpecForMIME(mimeType)
	if !ok {
		return nil
	}
	out := make([]string, 0, 1+len(spec.Fallbacks))
	out = append(out, spec.MIME)
	out = append(out, spec.Fallbacks...)
	return out
}

func isGoogleAppsMIME(mimeType string) bool {
	return strings.HasPrefix(mimeType, googleAppsPrefix)
}

// listableDriveFile reports whether a Drive item should appear as a migratable file.
func listableDriveFile(f *drive.File) bool {
	if f == nil || f.MimeType == "" {
		return false
	}
	if f.MimeType == mimeGoogleFolder {
		return false
	}
	effectiveMIME := effectiveContentMIME(f)
	if effectiveMIME == "" {
		return false
	}
	if isGoogleAppsMIME(effectiveMIME) {
		_, ok := exportSpecForMIME(effectiveMIME)
		return ok
	}
	return true
}

func effectiveContentMIME(f *drive.File) string {
	if f == nil {
		return ""
	}
	if f.MimeType == mimeGoogleShortcut && f.ShortcutDetails != nil {
		if f.ShortcutDetails.TargetMimeType != "" {
			return f.ShortcutDetails.TargetMimeType
		}
		// Shortcut without target MIME: OpenRead will resolve the target metadata.
		return mimeGoogleShortcut
	}
	return f.MimeType
}

func displayNameForExport(name, mimeType string) string {
	spec, ok := exportSpecForMIME(mimeType)
	if !ok {
		return name
	}
	name = stripMisleadingExtension(name, spec.Ext)
	return ensureExtension(name, spec.Ext)
}

// stripMisleadingExtension removes a user-facing extension that does not match the export target
// (e.g. a Google Form saved as "Feedback.pdf" exports as zip, not pdf).
func stripMisleadingExtension(name, exportExt string) string {
	got := strings.ToLower(filepath.Ext(name))
	want := strings.ToLower(exportExt)
	if got == "" || got == want {
		return name
	}
	switch got {
	case ".pdf", ".docx", ".xlsx", ".pptx", ".zip":
		return strings.TrimSuffix(name, filepath.Ext(name))
	default:
		return name
	}
}

func ensureExtension(name, ext string) string {
	if ext == "" {
		return name
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	lowerName := strings.ToLower(name)
	lowerExt := strings.ToLower(ext)
	if strings.HasSuffix(lowerName, lowerExt) {
		return name
	}
	return name + ext
}

func (d *DriveFS) openDriveFileContent(ctx context.Context, srv *drive.Service, fileID string) (io.ReadCloser, error) {
	meta, err := srv.Files.Get(fileID).
		Fields("mimeType,shortcutDetails(targetId,targetMimeType)").
		SupportsAllDrives(true).
		Do()
	if err != nil {
		return nil, err
	}

	contentID := fileID
	contentMIME := meta.MimeType
	if meta.MimeType == mimeGoogleShortcut {
		if meta.ShortcutDetails == nil || meta.ShortcutDetails.TargetId == "" {
			return nil, fmt.Errorf("google drive: shortcut %s has no target", fileID)
		}
		contentID = meta.ShortcutDetails.TargetId
		contentMIME = meta.ShortcutDetails.TargetMimeType
		if contentMIME == "" {
			target, err := srv.Files.Get(contentID).
				Fields("mimeType").
				SupportsAllDrives(true).
				Do()
			if err != nil {
				return nil, fmt.Errorf("google drive: resolve shortcut target %s: %w", contentID, err)
			}
			contentMIME = target.MimeType
		}
	}

	if specs := exportMIMEsForType(contentMIME); len(specs) > 0 {
		body, err := d.exportWorkspaceWithFallbacks(ctx, srv, contentID, specs)
		if err != nil {
			return nil, err
		}
		return body, nil
	}

	resp, err := srv.Files.Get(contentID).SupportsAllDrives(true).Download()
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
