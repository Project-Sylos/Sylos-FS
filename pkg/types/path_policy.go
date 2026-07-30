// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

import "errors"

// ErrPathBlocked is returned when an adapter refuses to traverse or open a path
// known to be unsafe or non-migratable (e.g. /proc, device namespaces).
// Callers should treat it as fatal: do not retry.
var ErrPathBlocked = errors.New("fs: path blocked from migration")
