// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types_test

import (
	"testing"

	localfs "codeberg.org/Sylos/Sylos-FS/pkg/fs/local"
	"codeberg.org/Sylos/Sylos-FS/pkg/types"
)

func TestListChildrenPaginationFromLocal(t *testing.T) {
	lim, ok := types.ListChildrenPaginationFrom(&localfs.LocalFS{})
	if !ok {
		t.Fatal("expected local FS to expose pagination")
	}
	if lim.MinPageSize != 20 || lim.MaxPageSize != 1000 || lim.DefaultPageSize != 100 {
		t.Fatalf("unexpected local limits: %+v", lim)
	}
	if lim.PreferLargePagesUnderThrottle {
		t.Fatal("local should not prefer large pages under throttle")
	}
}

type paginationStub struct {
	types.FSAdapter
}

func (paginationStub) ListChildrenPagination() types.ListChildrenPagination {
	return types.ListChildrenPagination{MinPageSize: 1, MaxPageSize: 99, DefaultPageSize: 10}
}

func TestListChildrenPaginationFromInterface(t *testing.T) {
	lim, ok := types.ListChildrenPaginationFrom(paginationStub{})
	if !ok {
		t.Fatal("expected stub to expose pagination")
	}
	if lim.MaxPageSize != 99 {
		t.Fatalf("got %+v", lim)
	}
}

func TestListChildrenPaginationFromNil(t *testing.T) {
	_, ok := types.ListChildrenPaginationFrom(nil)
	if ok {
		t.Fatal("nil adapter should not expose pagination")
	}
}
