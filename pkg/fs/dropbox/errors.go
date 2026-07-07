// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package dropbox

import (
	"encoding/json"
	"strings"
)

func extractDropboxErrorTag(errJSON json.RawMessage, errorSummary string) string {
	if tag := dropboxErrorTagFromSummary(errorSummary); tag != "" {
		return tag
	}
	return flattenDropboxErrorTag(errJSON)
}

// dropboxErrorTagFromSummary uses prefix matching on error_summary as recommended by Dropbox.
// https://developers.dropbox.com/error-handling-guide
func dropboxErrorTagFromSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	first := strings.Index(summary, "/")
	if first <= 0 {
		return summary
	}
	second := strings.Index(summary[first+1:], "/")
	if second >= 0 {
		return summary[:first+1+second]
	}
	return summary
}

func flattenDropboxErrorTag(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var tag taggedError
	if json.Unmarshal(raw, &tag) != nil || tag.Tag == "" {
		return ""
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return tag.Tag
	}
	if nested, ok := obj[tag.Tag]; ok && len(nested) > 0 {
		if child := flattenDropboxErrorTag(nested); child != "" {
			return tag.Tag + "/" + child
		}
	}
	return tag.Tag
}
