// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

// ListChildrenPagination describes provider-native bounds for in-memory ListChildren
// paging (ListPager page size). Cloud adapters expose API limits; local/Spectra use
// virtual paging over a full directory read.
type ListChildrenPagination struct {
	MinPageSize     int
	MaxPageSize     int
	DefaultPageSize int
	// PreferLargePagesUnderThrottle allows the autoscaler to grow page size when FS
	// rate limits are hit (fewer list calls per folder).
	PreferLargePagesUnderThrottle bool
}

// FSListChildrenPagination is implemented by FS adapters that expose list pagination
// limits for the migration engine autoscaler.
type FSListChildrenPagination interface {
	ListChildrenPagination() ListChildrenPagination
}

// ListChildrenPaginationFrom returns pagination limits when adapter implements them.
func ListChildrenPaginationFrom(adapter FSAdapter) (ListChildrenPagination, bool) {
	if adapter == nil {
		return ListChildrenPagination{}, false
	}
	if p, ok := adapter.(FSListChildrenPagination); ok && p != nil {
		lim := p.ListChildrenPagination()
		if lim.MaxPageSize > 0 || lim.MinPageSize > 0 || lim.DefaultPageSize > 0 {
			return lim, true
		}
	}
	return ListChildrenPagination{}, false
}
