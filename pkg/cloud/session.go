// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package cloud

import (
	"context"
	"fmt"
	"sync"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// AdapterFactory builds provider-specific FSAdapter instances from a cloud session.
type AdapterFactory interface {
	ProviderID() string
	NewSession(connectionID string, stored StoredCredentials, tokens *TokenStore, degradation *types.FSDegradationState) (Session, error)
	ListRoots(ctx context.Context, session Session) ([]Root, error)
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
