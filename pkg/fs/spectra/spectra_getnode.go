// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package spectra

import (
	"context"

	"codeberg.org/Sylos/Spectra/sdk"
)

func (s *SpectraFS) getNodeWithRetry(ctx context.Context, id string) (*sdk.Node, error) {
	var node *sdk.Node
	err := s.withClassifiedRetry(ctx, "GetNode", func() error {
		n, callErr := s.fs.GetNode(&sdk.GetNodeRequest{ID: id})
		if callErr != nil {
			return callErr
		}
		node = n
		return nil
	})
	if err != nil {
		return nil, err
	}
	return node, nil
}
