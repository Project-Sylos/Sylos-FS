// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package sftp

import (
	"fmt"
	"path"
	"strings"
)

func normalizeRemotePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	clean := path.Clean(p)
	if clean == "." {
		return "/"
	}
	return clean
}

func joinRemote(parent, name string) string {
	parent = normalizeRemotePath(parent)
	name = strings.Trim(name, "/")
	if parent == "/" {
		if name == "" {
			return "/"
		}
		return "/" + name
	}
	if name == "" {
		return parent
	}
	return path.Join(parent, name)
}

func parentRelPath(root, absPath string) string {
	root = strings.TrimSuffix(normalizeRemotePath(root), "/")
	absPath = normalizeRemotePath(absPath)
	if absPath == root || absPath == root+"/" {
		return "/"
	}
	prefix := root + "/"
	if strings.HasPrefix(absPath, prefix) {
		rel := strings.TrimPrefix(absPath, prefix)
		if rel == "" {
			return "/"
		}
		return "/" + rel
	}
	return "/"
}

func relativize(name, parentRelPath string) string {
	if parentRelPath == "/" {
		return "/" + name
	}
	return parentRelPath + "/" + name
}

func withinRoot(root, target string) (bool, error) {
	root = normalizeRemotePath(root)
	target = normalizeRemotePath(target)
	if root == target {
		return true, nil
	}
	prefix := strings.TrimSuffix(root, "/") + "/"
	if strings.HasPrefix(target+"/", prefix) || target == root {
		return true, nil
	}
	return false, fmt.Errorf("path %q escapes root %q", target, root)
}
