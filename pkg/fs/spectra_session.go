// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package fs

import (
	"fmt"
	"sync"

	"github.com/Project-Sylos/Spectra/sdk"
)

// SpectraSession manages a single Spectra SDK instance and provides adapters for it.
// This is the ONLY place where sdk.New() is allowed to be called.
type SpectraSession struct {
	mu         sync.RWMutex
	spectraFS  *sdk.SpectraFS
	configPath string
	closed     bool
}

// NewSpectraSession creates a new Spectra session by calling sdk.New().
// This is the ONLY place in the codebase where sdk.New() should be called.
func NewSpectraSession(configPath string) (*SpectraSession, error) {
	spectraFS, err := sdk.New(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create Spectra session: %w", err)
	}

	return &SpectraSession{
		spectraFS:  spectraFS,
		configPath: configPath,
		closed:     false,
	}, nil
}

// Close closes the Spectra SDK instance. Safe to call multiple times.
func (s *SpectraSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil // Already closed, safe to ignore
	}

	if s.spectraFS != nil {
		if err := s.spectraFS.Close(); err != nil {
			s.closed = true // Mark as closed even on error
			return fmt.Errorf("failed to close Spectra session: %w", err)
		}
	}

	s.closed = true
	return nil
}

// IsClosed returns whether the session is closed.
func (s *SpectraSession) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// CreateAdapter creates a SpectraFS adapter for the given rootID and world.
// The adapter does not own the lifecycle of the SDK instance.
// Returns an error immediately if the session is closed (fail fast).
func (s *SpectraSession) CreateAdapter(rootID, world string) (*SpectraFS, error) {
	s.mu.RLock()
	closed := s.closed
	spectraFS := s.spectraFS
	s.mu.RUnlock()

	if closed {
		return nil, fmt.Errorf("cannot create adapter: session is closed")
	}

	if spectraFS == nil {
		return nil, fmt.Errorf("cannot create adapter: session has no SpectraFS instance")
	}

	// Create adapter using the session's SpectraFS instance
	// Note: We don't validate the root node here - validation happens on first use
	adapter, err := NewSpectraFS(spectraFS, rootID, world)
	if err != nil {
		return nil, fmt.Errorf("failed to create adapter: %w", err)
	}

	return adapter, nil
}

// GetConfigPath returns the config path used to create this session.
func (s *SpectraSession) GetConfigPath() string {
	return s.configPath
}
