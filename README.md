# Sylos-FS

A standardized filesystem adapter library for the Sylos project. Applications (Sylos-API, Migration Engine) talk to storage through a shared `FSAdapter` interface so local disk, SFTP, Spectra, and cloud providers share one contract.

## Overview

- **Adapters**: Local, SFTP, Spectra, Google Drive, Dropbox, Box, OneDrive, SharePoint (Graph)
- **Cloud sessions**: OAuth stored credentials, token refresh as FS middleware, shared `FSDegradationState`
- **Service manager**: connection pooling / refcount, browse roots, pagination
- **Streaming I/O**: `OpenRead` / `OpenWrite` move bytes in small chunks — **no full-file spill-to-temp** before upload

## Package structure

```
pkg/
├── types/           # FSAdapter, Folder/File, degradation, pagination, optional batch/resume/size interfaces
├── cloud/           # Provider IDs, OAuth store helpers, browse roots
├── credentials/     # Classified retry helpers
└── fs/
    ├── manager*.go  # ServiceManager
    ├── ctxstream/   # Context-aware pipe wrappers (Dropbox/GDrive/Box streaming)
    ├── local/       # LocalFS (+ network/FUSE paths as local)
    ├── sftp/
    ├── spectra/
    ├── googledrive/
    ├── dropbox/
    ├── box/
    ├── msgraph/     # Shared Microsoft Graph client + streaming upload writer
    ├── onedrive/
    └── sharepoint/
```

See **[pkg/fs/README.md](./pkg/fs/README.md)** for adapter usage patterns and **[docs/cloud_provider_checklist.md](./docs/cloud_provider_checklist.md)** when adding a provider.

## Upload streaming contract

Migration Engine copies with a tight read/write loop. Destination writers must:

1. Accept bytes on `Write` without staging the entire object to disk.
2. Upload provider fragments/parts as buffers fill (or concurrently via `io.Pipe` like Dropbox/Drive).
3. Use `Close` to finish/commit the session (and flush a trailing partial fragment), not to perform the only network transfer.

Optional **`FSOpenWriteWithSize`** (`OpenWriteWithSize(ctx, fileID, size)`) supplies declared length for APIs that require it up front (Box chunked sessions). ME calls this when implemented.

Small in-memory buffers for one fragment (e.g. ≤5–8 MiB Graph/Box part, or ≤4 MiB Graph simple PUT) are fine. **Temp-file spill of the whole upload is not.**

## Quick Start

### Basic Usage

```go
import (
    "context"
    "io"
    "strings"
    "codeberg.org/Sylos/Sylos-FS/pkg/fs"
    "codeberg.org/Sylos/Sylos-FS/pkg/types"
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
import "codeberg.org/Sylos/Sylos-FS/pkg/fs/local"

// Create a local filesystem adapter
localFS, err := local.NewLocalFS("/path/to/root")
if err != nil {
    log.Fatal(err)
}

// List children
result, err := localFS.ListChildren("/path/to/directory")
if err != nil {
    log.Fatal(err)
}

// Open a file for reading
ctx := context.Background()
reader, err := localFS.OpenRead(ctx, "/path/to/file.txt")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

// Create a folder
folder, err := localFS.CreateFolder("/path/to/parent", "new-folder")
if err != nil {
    log.Fatal(err)
}

// Create a file with metadata, then write to it
file, err := localFS.CreateFile(ctx, "/path/to/parent", "new-file.txt", 0, nil)
if err != nil {
    log.Fatal(err)
}

// Open the file for writing
writer, err := localFS.OpenWrite(ctx, file.ServiceID)
if err != nil {
    log.Fatal(err)
}
defer writer.Close()

// Worker copies data (e.g., from another reader)
fileContent := strings.NewReader("file content")
_, err = io.Copy(writer, fileContent)
if err != nil {
    log.Fatal(err)
}

// Close commits the write
if err := writer.Close(); err != nil {
    log.Fatal(err)
}
```

## Features

### Filesystem adapters

- **LocalFS** — OS filesystem (physical, network mounts, FUSE/WinFSP as local paths)
- **SFTP** — remote SSH filesystem
- **SpectraFS** — synthetic tree for chaos / integration tests
- **Cloud** — Google Drive, Dropbox, Box, OneDrive, SharePoint (see `docs/cloud_provider_checklist.md`)

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

- `codeberg.org/Sylos/Spectra/sdk` - Spectra filesystem SDK

## License

MIT License
