// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

type policyTestFactory struct {
	ids []string
}

func (policyTestFactory) ProviderID() string { return "policy_test" }
func (f policyTestFactory) ForbiddenMigrationRootIDs() []string {
	return f.ids
}
func (policyTestFactory) NewSession(string, StoredCredentials, *TokenStore, *types.FSDegradationState) (Session, error) {
	return nil, nil
}
func (policyTestFactory) ListRoots(context.Context, Session) ([]Root, error) {
	return nil, nil
}

func TestValidateMigrationRootUsesProviderPolicy(t *testing.T) {
	RegisterFactory(policyTestFactory{ids: []string{"virtual"}})

	if err := ValidateMigrationRootFolder("policy_test", types.Folder{ServiceID: "real-folder"}); err != nil {
		t.Fatalf("real folder rejected: %v", err)
	}
	if err := ValidateMigrationRootFolder("policy_test", types.Folder{ServiceID: "virtual"}); !errors.Is(err, ErrForbiddenMigrationRoot) {
		t.Fatalf("expected ErrForbiddenMigrationRoot, got %v", err)
	}
}

func TestForbiddenMigrationRootIDsReturnsCopy(t *testing.T) {
	RegisterFactory(policyTestFactory{ids: []string{"virtual"}})

	ids, err := ForbiddenMigrationRootIDs("policy_test")
	if err != nil {
		t.Fatal(err)
	}
	ids[0] = "mutated"

	if err := ValidateMigrationRootFolder("policy_test", types.Folder{ServiceID: "virtual"}); !errors.Is(err, ErrForbiddenMigrationRoot) {
		t.Fatalf("provider policy was mutated: %v", err)
	}
}
