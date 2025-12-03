# Sylos-FS

A standardized filesystem adapter library for the Sylos project. This repository provides a unified interface for interacting with different filesystem types (local filesystem and Spectra filesystem simulator) that can be used by both the API and migration-engine repositories.

## Overview

Sylos-FS abstracts filesystem operations behind a common `FSAdapter` interface, allowing applications to work with different storage backends without changing their core logic. The library provides:

- **Filesystem Adapters**: Implementations for local filesystem and Spectra filesystem
- **Service Management**: Centralized service configuration and lifecycle management
- **Connection Pooling**: Efficient resource management for Spectra connections
- **Pagination Support**: Built-in pagination for listing directory contents
- **Virtual Services**: Support for virtual service IDs ("local" and "spectra")

## Package Structure

```
pkg/
├── types/          # Shared types, interfaces, and utilities
│   └── types.go    # Core types (Folder, File, FSAdapter, etc.)
└── fs/             # Filesystem adapters and service manager
    ├── local.go    # Local filesystem adapter
    ├── spectra.go  # Spectra filesystem adapter
    └── manager.go # Service manager for coordinating services
```

## Quick Start

### Basic Usage

```go
import (
    "github.com/Project-Sylos/Sylos-FS/pkg/fs"
    "github.com/Project-Sylos/Sylos-FS/pkg/types"
)

// Create a service manager
manager := fs.NewServiceManager()

// Load services
localServices := []types.LocalServiceConfig{
    {
        ID:       "local-1",
        Name:     "Local Filesystem",
        RootPath: "/path/to/root", // Empty for unrestricted access
    },
}

spectraServices := []types.SpectraServiceConfig{
    {
        ID:         "spectra-1",
        Name:       "Spectra Primary",
        World:      "primary",
        RootID:     "root",
        ConfigPath: "/path/to/spectra/config",
    },
}

err := manager.LoadServices(localServices, spectraServices)
if err != nil {
    log.Fatal(err)
}

// List available sources
sources, err := manager.ListSources(context.Background())
if err != nil {
    log.Fatal(err)
}

// List children of a service
result, pagination, err := manager.ListChildren(context.Background(), types.ListChildrenRequest{
    ServiceID:   "local-1",
    Identifier:  "/path/to/directory",
    Offset:      0,
    Limit:       100,
    FoldersOnly: false,
})
```

### Direct Adapter Usage

```go
// Create a local filesystem adapter
localFS, err := fs.NewLocalFS("/path/to/root")
if err != nil {
    log.Fatal(err)
}

// List children
result, err := localFS.ListChildren("/path/to/directory")
if err != nil {
    log.Fatal(err)
}

// Download a file
reader, err := localFS.DownloadFile("/path/to/file.txt")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

// Create a folder
folder, err := localFS.CreateFolder("/path/to/parent", "new-folder")
if err != nil {
    log.Fatal(err)
}

// Upload a file
fileContent := strings.NewReader("file content")
file, err := localFS.UploadFile("/path/to/destination/file.txt", fileContent)
if err != nil {
    log.Fatal(err)
}
```

## Features

### Filesystem Adapters

- **LocalFS**: Interacts with the local operating system filesystem
  - Supports Windows and Unix-like systems
  - Handles path normalization across platforms
  - Supports restricted and unrestricted browsing modes

- **SpectraFS**: Interacts with the Spectra filesystem simulator
  - Multi-world support (primary, s1, s2, etc.)
  - Node-based filesystem operations
  - Connection pooling for efficient resource usage

### Service Manager

The `ServiceManager` provides:

- **Service Configuration**: Load and manage multiple service definitions
- **Virtual Services**: Use "local" or "spectra" as virtual service IDs
- **Pagination**: Automatic pagination for directory listings
- **Connection Management**: Reference-counted connection pooling for Spectra
- **Drive Listing**: List available drives/volumes (Windows and Unix)

### Pagination

The library supports flexible pagination:

```go
req := types.ListChildrenRequest{
    ServiceID:   "local-1",
    Identifier:  "/path/to/directory",
    Offset:      0,        // Start from beginning
    Limit:       50,        // 50 items per page
    FoldersOnly: false,     // Include both folders and files
}

result, pagination, err := manager.ListChildren(ctx, req)
// pagination.Total - total number of items
// pagination.HasMore - whether more items exist
// pagination.Offset, pagination.Limit - current pagination state
```

## Virtual Services

The library supports virtual service IDs for convenience:

- **"local"**: Maps to the first available local service, with unrestricted browsing
- **"spectra"**: Maps to Spectra services based on role:
  - `role="source"` → world "primary"
  - `role="destination"` → world "s1"

## Connection Pooling

For Spectra services, the library manages connections efficiently:

```go
// Acquire an adapter with connection pooling
adapter, release, err := manager.AcquireAdapter(def, "root", "connection-id")
if err != nil {
    log.Fatal(err)
}
defer release() // Release the connection when done

// Use the adapter
result, err := adapter.ListChildren("node-id")
```

## Path Normalization

The library normalizes paths consistently:

- Forward slashes are used for logical paths
- Paths are cleaned and deduplicated
- Root-relative paths are maintained for portability

## Error Handling

Common errors:

- `fs.ErrServiceNotFound`: Service with the given ID doesn't exist
- Path validation errors when accessing restricted directories
- Filesystem errors from underlying operations

## Dependencies

- `github.com/Project-Sylos/Spectra/sdk` - Spectra filesystem SDK

## License

MIT License
