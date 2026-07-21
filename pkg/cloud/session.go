// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// AdapterFactory builds provider-specific FSAdapter instances from a cloud session.
type AdapterFactory interface {
	ProviderID() string
	ForbiddenMigrationRootIDs() []string
	NewSession(connectionID string, stored StoredCredentials, tokens *TokenStore, degradation *types.FSDegradationState) (Session, error)
	ListRoots(ctx context.Context, session Session) ([]Root, error)
}

var ErrForbiddenMigrationRoot = errors.New("cloud: forbidden migration root")

// ForbiddenMigrationRootIDs returns a copy of the provider-owned virtual root IDs
// that may be browsed but cannot be selected as migration roots.
func ForbiddenMigrationRootIDs(providerID string) ([]string, error) {
	factory, err := Factory(providerID)
	if err != nil {
		return nil, err
	}
	return append([]string(nil), factory.ForbiddenMigrationRootIDs()...), nil
}

// ValidateMigrationRoot rejects provider virtual containers that are not real
// filesystem roots. Real folders inside those containers remain valid.
func ValidateMigrationRoot(providerID, rootID string) error {
	return ValidateMigrationRootFolder(providerID, types.Folder{ServiceID: rootID})
}

// ValidateMigrationRootFolder rejects browse-only virtual containers by id or root type
// (e.g. SharePoint sites with dynamic ids).
func ValidateMigrationRootFolder(providerID string, root types.Folder) error {
	switch root.Type {
	case RootTypeSharePointSite, RootTypeSharedWithMe, RootTypeTeamSpace:
		return fmt.Errorf("%w: provider %q root %q (type %q) cannot be used as a source or destination root",
			ErrForbiddenMigrationRoot, providerID, root.ServiceID, root.Type)
	}
	ids, err := ForbiddenMigrationRootIDs(providerID)
	if err != nil {
		return err
	}
	for _, forbiddenID := range ids {
		if root.ServiceID == forbiddenID {
			return fmt.Errorf("%w: provider %q root %q cannot be used as a source or destination root", ErrForbiddenMigrationRoot, providerID, root.ServiceID)
		}
	}
	return nil
}

// Session is a ref-counted cloud connection with shared degradation telemetry.
type Session interface {
	ConnectionID() string
	ProviderID() string
	DegradationState() *types.FSDegradationState
	CreateAdapter(rootFolder types.Folder) (types.FSAdapter, error)
	RefreshAccessToken(ctx context.Context) error
	HasValidCredentials() bool
	Close() error
}

// CredentialsExporter is implemented by sessions that can re-export StoredCredentials
// for persistence into the migration DB (e.g. after SetRoot creates the migration).
type CredentialsExporter interface {
	ExportStoredCredentials() (StoredCredentials, error)
}

// CredentialsPersister is implemented by sessions that must write back rotated
// refresh tokens (e.g. Box single-use refresh tokens).
type CredentialsPersister interface {
	SetCredentialsPersist(fn func(StoredCredentials) error)
}

var (
	factoryMu sync.RWMutex
	factories = map[string]AdapterFactory{}
)

// RegisterFactory registers a cloud adapter factory for a provider ID.
func RegisterFactory(factory AdapterFactory) {
	if factory == nil {
		return
	}
	factoryMu.Lock()
	defer factoryMu.Unlock()
	factories[factory.ProviderID()] = factory
}

// Factory returns the registered factory for a provider.
func Factory(providerID string) (AdapterFactory, error) {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	f, ok := factories[providerID]
	if !ok {
		return nil, fmt.Errorf("cloud: unsupported provider %q", providerID)
	}
	return f, nil
}

// ListRegisteredProviders returns provider IDs with registered factories.
func ListRegisteredProviders() []string {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	out := make([]string, 0, len(factories))
	for id := range factories {
		out = append(out, id)
	}
	return out
}
