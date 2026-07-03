// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package local

// InjectBeforeOp, when set (tests only), runs before each classified-retry attempt.
// Return non-nil to inject that error instead of calling the underlying op.
func (l *LocalFS) InjectBeforeOp(fn func(operation string, attempt int) error) {
	l.injectBeforeOp = fn
}

func (l *LocalFS) clearInject() {
	l.injectBeforeOp = nil
}
