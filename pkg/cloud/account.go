// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import "context"

// AccountIdentity is the signed-in cloud account shown in the UI.
type AccountIdentity struct {
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// AccountResolver is implemented by sessions that can report who is signed in.
type AccountResolver interface {
	ResolveAccountIdentity(ctx context.Context) (AccountIdentity, error)
}

// Label prefers email, then display name.
func (a AccountIdentity) Label() string {
	if a.Email != "" {
		return a.Email
	}
	return a.DisplayName
}
