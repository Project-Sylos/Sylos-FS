// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

// Package spectra implements FSAdapter for the Spectra filesystem simulator.
package spectra

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"codeberg.org/Sylos/Spectra/sdk"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// SpectraSession manages a single Spectra SDK instance and provides adapters for it.
// This is the ONLY place where sdk.New() is allowed to be called.
type SpectraSession struct {
	mu          sync.RWMutex
	spectraFS   *sdk.SpectraFS
	configPath  string
	closed      bool
	isEphemeral bool
	degradation *types.FSDegradationState
}

type spectraConfig struct {
	Mode            string             `json:"mode"`
	SecondaryTables map[string]float64 `json:"secondary_tables"`
	Auth            *struct {
		Enabled bool `json:"enabled"`
	} `json:"auth"`
}

// NewSpectraSession creates a new Spectra session by calling sdk.New().
func NewSpectraSession(configPath string) (*SpectraSession, error) {
	cfgMeta, isEphemeral, err := readSpectraConfigMeta(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Spectra config mode: %w", err)
	}

	spectraFS, err := sdk.New(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create Spectra session: %w", err)
	}

	if spectraFS.AuthEnabled() {
		worlds := []string{"primary"}
		for world := range cfgMeta.SecondaryTables {
			worlds = append(worlds, world)
		}
		for _, world := range worlds {
			if _, err := spectraFS.EnsureWorldAuth(world); err != nil {
				_ = spectraFS.Close()
				return nil, fmt.Errorf("ensure auth for world %s: %w", world, err)
			}
		}
	}

	return &SpectraSession{
		spectraFS:   spectraFS,
		configPath:  configPath,
		closed:      false,
		isEphemeral: isEphemeral,
		degradation: types.NewFSDegradationState(),
	}, nil
}

func readSpectraConfigMeta(configPath string) (spectraConfig, bool, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return spectraConfig{}, false, fmt.Errorf("failed to read config file: %w", err)
	}

	var config spectraConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return spectraConfig{}, false, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, config.Mode == "ephemeral", nil
}

// Close closes the Spectra SDK instance.
func (s *SpectraSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	if s.spectraFS != nil {
		if err := s.spectraFS.Close(); err != nil {
			s.closed = true
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
func (s *SpectraSession) CreateAdapter(rootID, world string) (*SpectraFS, error) {
	s.mu.RLock()
	closed := s.closed
	spectraFS := s.spectraFS
	isEphemeral := s.isEphemeral
	s.mu.RUnlock()

	if closed {
		return nil, fmt.Errorf("cannot create adapter: session is closed")
	}

	if spectraFS == nil {
		return nil, fmt.Errorf("cannot create adapter: session has no SpectraFS instance")
	}

	if world == "" {
		world = "primary"
	}
	if spectraFS.AuthEnabled() {
		if _, err := spectraFS.EnsureWorldAuth(world); err != nil {
			return nil, fmt.Errorf("ensure auth for world %s: %w", world, err)
		}
	}

	adapter, err := NewSpectraFS(spectraFS, rootID, world, isEphemeral, WithDegradationState(s.degradation))
	if err != nil {
		return nil, fmt.Errorf("failed to create adapter: %w", err)
	}

	return adapter, nil
}

// DegradationState returns shared session degradation telemetry.
func (s *SpectraSession) DegradationState() *types.FSDegradationState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.degradation
}

// GetConfigPath returns the config path used to create this session.
func (s *SpectraSession) GetConfigPath() string {
	return s.configPath
}

// IsEphemeral returns whether this session is in ephemeral mode.
func (s *SpectraSession) IsEphemeral() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isEphemeral
}

// GetSDKInstance returns the shared Spectra SDK (for tests / advanced wiring).
func (s *SpectraSession) GetSDKInstance() *sdk.SpectraFS {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.spectraFS
}
