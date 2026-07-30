# fs Package

The `fs` package provides filesystem adapters and service management for the Sylos project. It implements a unified interface for interacting with different filesystem types, including local filesystems and the Spectra filesystem simulator.

## Overview

Subpackages and the root `fs` package:

- **`fs`**: `ServiceManager`, `GetCopyBuffer`, service loading
- **`fs/local`**: `LocalFS` adapter (Lstat-gated listing, context-aware streams)
- **`fs/spectra`**: `SpectraSession`, `SpectraFS` adapter
- **`fs/ctxstream`**: Context-aware `io.ReadCloser` / `io.WriteCloser` wrappers
- **`credentials`** (sibling package `pkg/credentials`): AES-256-GCM encrypt/decrypt, HKDF per-connection keys, `DoWithAuthRetry` for cloud adapters

All adapters implement the `types.FSAdapter` interface, providing a consistent API regardless of the underlying storage backend.

### Encrypted credentials and HKDF (`pkg/credentials`)

The engine stores one **envelope master key** (32 random bytes from `credentials.GenerateMasterKey`) and stable **connection IDs** in its own store. It does **not** need a secret map from key hash to file path.

- **`DeriveConnectionKey(masterKey, connectionID)`** — HKDF-SHA256 (`salt` = connection ID, `info` = `credentials.HKDFInfo`). Use the 32-byte result with **`Encrypt`** / **`Decrypt`** for `creds.conf` blobs. Wrong master key or connection ID fails at decrypt (GCM auth). Empty `connectionID` is rejected.
- **Migration**: Blobs created with the raw master key (no HKDF) are **not** compatible with derived keys; re-encrypt if anything was shipped that way.
- **`FSAdapter.Initialize(masterKey, connectionID)`** and **`RegisterCredentials(data, masterKey, connectionID)`** — cloud adapters derive the key internally; **`LocalFS`** / **`SpectraFS`** ignore these (no-ops).
- **`DoWithClassifiedRetry` / `DoWithAuthRetry`** — FS middleware around adapter I/O: on auth failure run **`Refresh` once** then retry the op; on rate limit sleep (capped) and retry. Workers only call normal FS methods and block until success or a classified error (including auth after refresh fails). **LocalFS** has no token refresh. Cloud + Spectra wire `IsAuthFailure` + `Refresh`.

## Filesystem Adapters

### LocalFS

The `LocalFS` adapter provides access to the local operating system filesystem. It handles path normalization, cross-platform compatibility, and root-relative path tracking.

#### Features

- **Cross-platform support**: Works on Windows and Unix-like systems
- **Path normalization**: Automatically normalizes paths to use forward slashes
- **Root-relative paths**: Maintains logical paths relative to a configured root
- **Unrestricted browsing**: Can be configured to allow browsing outside the root path
- **Optional page cache hints**: See below

#### Page cache hints (POSIX fadvise)

During bulk copy, Linux (and some other Unix kernels) grow the **page cache** aggressively—RSS can jump far beyond your user-space buffers. That cache is reclaimable but can still pressure memory.

`LocalFS.PageCacheHints` (default `false`) opts into **posix_fadvise** on the **read** path only:

- After `OpenRead`: `FADV_SEQUENTIAL`—kernel can tune read-ahead for sequential access.
- On `Close` after reading: `FADV_DONTNEED`—hint that cached pages for that file need not be kept.

These calls are **per-fd hints** and **do not require root**—safe for a locked-down service account. They are no-ops on platforms without fadvise support (stub build).

**Not covered here** (system-wide, typically **root**): `vm.dirty_ratio`, `vm.dirty_background_ratio`, `drop_caches`, etc. Use those only when you control the host.

Example:

```go
localFS, _ := local.NewLocalFS("/data")
localFS.PageCacheHints = true
// OpenRead/Close on this adapter will issue fadvise where supported
```

#### Example

```go
import (
    "context"
    "fmt"
    "io"
    "log"
    "strings"
    "codeberg.org/Sylos/Sylos-FS/pkg/fs/local"
    "codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// Create a local filesystem adapter rooted at /home/user
localFS, err := local.NewLocalFS("/home/user")
if err != nil {
    log.Fatal(err)
}

// List children of a directory
result, err := localFS.ListChildren("/home/user/documents")
if err != nil {
    log.Fatal(err)
}

// Process folders
for _, folder := range result.Folders {
    fmt.Printf("Folder: %s (path: %s)\n", folder.DisplayName, folder.LocationPath)
}

// Process files
for _, file := range result.Files {
    fmt.Printf("File: %s (size: %d bytes)\n", file.DisplayName, file.Size)
}

// Open a file for reading (worker owns the copy loop)
ctx := context.Background()
reader, err := localFS.OpenRead(ctx, "/home/user/documents/file.txt")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

// Worker reads from the stream (e.g., using io.Copy to another writer)
data, err := io.ReadAll(reader)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Read %d bytes\n", len(data))

// Create a new folder
folder, err := localFS.CreateFolder("/home/user/documents", "new-folder")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Created folder: %s\n", folder.LocationPath)

// Create a file with metadata, then write to it
file, err := localFS.CreateFile(ctx, "/home/user/documents", "new-file.txt", 0, nil)
if err != nil {
    log.Fatal(err)
}

// Open the file for writing
writer, err := localFS.OpenWrite(ctx, file.ServiceID)
if err != nil {
    log.Fatal(err)
}
defer writer.Close()

// Worker writes to the stream (e.g., using io.Copy from a reader)
content := strings.NewReader("file content here")
_, err = io.Copy(writer, content)
if err != nil {
    log.Fatal(err)
}

// Close commits the write
if err := writer.Close(); err != nil {
    log.Fatal(err)
}
fmt.Printf("Uploaded file: %s\n", file.DisplayName)
```

#### Path Handling

The `LocalFS` adapter maintains two types of paths:

- **Physical paths**: Absolute paths on the filesystem (e.g., `/home/user/documents/file.txt`)
- **Location paths**: Root-relative logical paths (e.g., `/documents/file.txt`)

When you create a `LocalFS` with root `/home/user`, all operations maintain paths relative to that root. This allows for portable path references that work regardless of where the root is located.

### SpectraFS

The `SpectraFS` adapter provides access to the Spectra filesystem simulator. It works with node-based operations and supports multiple "worlds" for different data contexts.

#### Features

- **Multi-world support**: Can operate in different worlds (primary, s1, s2, etc.)
- **Node-based operations**: Uses node IDs instead of file paths
- **Connection sharing**: Can share SDK instances across multiple adapters
- **Automatic validation**: Validates root nodes and folder types

#### Example

```go
import (
    "fmt"
    "io"
    "log"
    "codeberg.org/Sylos/Sylos-FS/pkg/fs/spectra"
    "codeberg.org/Sylos/Spectra/sdk"
)

// Create a Spectra SDK instance (or use spectra.NewSpectraSession for managed lifecycle)
spectraSDK, err := sdk.New("/path/to/spectra/config")
if err != nil {
    log.Fatal(err)
}
defer spectraSDK.Close()

// Create a SpectraFS adapter
spectraFS, err := spectra.NewSpectraFS(spectraSDK, "root", "primary", false)
if err != nil {
    log.Fatal(err)
}

// List children of the root node
result, err := spectraFS.ListChildren("root")
if err != nil {
    log.Fatal(err)
}

// Create a folder
folder, err := spectraFS.CreateFolder("root", "my-folder")
if err != nil {
    log.Fatal(err)
}

// Open a file for reading (worker owns the copy loop)
ctx := context.Background()
reader, err := spectraFS.OpenRead(ctx, file.ServiceID)
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

// Worker reads from the stream
data, err := io.ReadAll(reader)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Read %d bytes\n", len(data))

// Create a file with metadata, then write to it
newFile, err := spectraFS.CreateFile(ctx, folder.ServiceID, "new-file.txt", 0, nil)
if err != nil {
    log.Fatal(err)
}

// Open the file for writing
writer, err := spectraFS.OpenWrite(ctx, newFile.ServiceID)
if err != nil {
    log.Fatal(err)
}
defer writer.Close()

// Worker writes to the stream
content := strings.NewReader("file content")
_, err = io.Copy(writer, content)
if err != nil {
    log.Fatal(err)
}

// Close commits the write
if err := writer.Close(); err != nil {
    log.Fatal(err)
}
```

#### Worlds

Spectra supports multiple "worlds" which are separate data contexts:

- **primary**: The main world, typically used as the source
- **s1, s2, etc.**: Secondary worlds, typically used as destinations

When creating a `SpectraFS` adapter, you specify which world to operate in. All operations (list, create, upload) will be scoped to that world.

## Streaming Operations

Both `LocalFS` and `SpectraFS` support streaming operations where the worker owns the copy loop. FS adapters expose streams (`io.ReadCloser` for reads, `io.WriteCloser` for writes), and the worker uses standard `io.Copy` to transfer data.

### Key Principles

1. **Workers own the loop**: Worker code does `io.Copy(dstWriter, srcReader)`, not the FS layer
2. **FS implementations handle buffering**: If a provider can't stream, it buffers internally (memory or disk)
3. **Close commits**: `WriteCloser.Close()` finalizes the upload; if it fails, upload failed
4. **Standard interfaces**: Uses standard `io.Reader`/`io.Writer` interfaces

### Worker Pattern

The standard pattern for copying files between adapters:

```go
import (
    "context"
    "io"
    "log"

    "codeberg.org/Sylos/Sylos-FS/pkg/fs"
)

ctx := context.Background()

// Open source file for reading
srcReader, err := srcAdapter.OpenRead(ctx, srcFileID)
if err != nil {
    log.Fatal(err)
}
defer srcReader.Close()

// Create destination file with metadata
dstFile, err := dstAdapter.CreateFile(ctx, parentID, name, size, metadata)
if err != nil {
    log.Fatal(err)
}

// Open destination file for writing
dstWriter, err := dstAdapter.OpenWrite(ctx, dstFile.ServiceID)
if err != nil {
    log.Fatal(err)
}
defer dstWriter.Close()

// Worker owns the copy loop
// Use GetCopyBuffer() for optimal performance (default 8MB, or specify custom size)
buffer := fs.GetCopyBuffer(0) // 0 = use default 8MB
_, err = io.CopyBuffer(dstWriter, srcReader, buffer)
if err != nil {
    log.Fatal(err)
}

// Close commits the write
if err := dstWriter.Close(); err != nil {
    log.Fatal(err)
}
```

### Implementation Details

- **LocalFS**: `OpenRead` / `OpenWrite` use OS file handles (direct streaming).
- **Cloud (Dropbox, Drive, Box, Graph)**: stream upload sessions on `Write` / pipe; `Close` finishes the session.
- **SpectraFS**: `OpenWrite` accepts the stream without local staging (SDK does not persist upload bytes).

## Service Manager

The `ServiceManager` provides centralized management of multiple filesystem services. It handles service configuration, virtual service mapping, pagination, and connection pooling.

### Key Features

- **Service Configuration**: Load and manage multiple service definitions
- **Virtual Services**: Support for "local" and "spectra" virtual service IDs
- **Pagination**: Automatic pagination for directory listings
- **Connection Pooling**: Reference-counted connection management for Spectra
- **Drive Listing**: List available drives/volumes on the system

### Example: Basic Service Management

```go
import (
    "context"
    "codeberg.org/Sylos/Sylos-FS/pkg/fs"
    "codeberg.org/Sylos/Sylos-FS/pkg/types"
)

// Create a service manager
manager := fs.NewServiceManager()

// Configure services
localServices := []types.LocalServiceConfig{
    {
        ID:       "local-main",
        Name:     "Main Local Storage",
        RootPath: "/data", // Empty for unrestricted access
    },
}

spectraServices := []types.SpectraServiceConfig{
    {
        ID:         "spectra-primary",
        Name:       "Spectra Primary World",
        World:      "primary",
        RootID:     "root",
        ConfigPath: "/etc/spectra/config.json",
    },
    {
        ID:         "spectra-s1",
        Name:       "Spectra Secondary World",
        World:      "s1",
        RootID:     "root",
        ConfigPath: "/etc/spectra/config.json",
    },
}

// Load services
err := manager.LoadServices(localServices, spectraServices)
if err != nil {
    log.Fatal(err)
}
```

### Example: Listing Sources

```go
// List all available sources
ctx := context.Background()
sources, err := manager.ListSources(ctx)
if err != nil {
    log.Fatal(err)
}

for _, source := range sources {
    fmt.Printf("Source: %s (type: %s, id: %s)\n", 
        source.DisplayName, source.Type, source.ID)
}
```

### Example: Listing Children with Pagination

```go
// List children with pagination
req := types.ListChildrenRequest{
    ServiceID:   "local-main",
    Identifier: "/data/documents",
    Offset:     0,
    Limit:      50,
    FoldersOnly: false,
}

result, pagination, err := manager.ListChildren(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Total: %d (folders: %d, files: %d)\n",
    pagination.Total, pagination.TotalFolders, pagination.TotalFiles)
fmt.Printf("Has more: %v\n", pagination.HasMore)

// Process results
for _, folder := range result.Folders {
    fmt.Printf("Folder: %s\n", folder.DisplayName)
}

for _, file := range result.Files {
    fmt.Printf("File: %s (%d bytes)\n", file.DisplayName, file.Size)
}

// Get next page
if pagination.HasMore {
    req.Offset = pagination.Offset + pagination.Limit
    result, pagination, err = manager.ListChildren(ctx, req)
    // ...
}
```

### Example: Using Virtual Services

```go
// Use "local" virtual service (unrestricted browsing)
req := types.ListChildrenRequest{
    ServiceID:   "local",  // Virtual service ID
    Identifier:  "C:\\",   // Can browse anywhere
    Offset:      0,
    Limit:       100,
    FoldersOnly: false,
}
result, pagination, err := manager.ListChildren(ctx, req)

// Use "spectra" virtual service with role-based world mapping
req = types.ListChildrenRequest{
    ServiceID:   "spectra",
    Identifier:  "root",
    Role:        "source",      // Maps to "primary" world
    Offset:      0,
    Limit:       100,
    FoldersOnly: false,
}
result, pagination, err = manager.ListChildren(ctx, req)
```

### Example: Listing Drives

```go
// List available drives (Windows: C:\, D:\, etc. | Unix: /, /mnt, etc.)
drives, err := manager.ListDrives(ctx, "local")
if err != nil {
    log.Fatal(err)
}

for _, drive := range drives {
    fmt.Printf("Drive: %s (%s) - %s\n",
        drive.DisplayName, drive.Path, drive.Type)
}
```

### Example: Acquiring Adapters with Connection Pooling

```go
// Get a service definition
def, err := manager.GetServiceDefinition("spectra-primary")
if err != nil {
    log.Fatal(err)
}

// Acquire an adapter with connection pooling
// The same connectionID will reuse the same Spectra SDK instance
adapter, release, err := manager.AcquireAdapter(def, "root", "migration-1")
if err != nil {
    log.Fatal(err)
}
defer release() // Important: release when done

// Use the adapter
result, err := adapter.ListChildren("root")
if err != nil {
    log.Fatal(err)
}

// Acquire another adapter with the same connection ID
// This will reuse the same Spectra SDK instance
adapter2, release2, err := manager.AcquireAdapter(def, "root", "migration-1")
if err != nil {
    log.Fatal(err)
}
defer release2()

// Both adapters share the same underlying connection
// The connection is closed when all references are released
```

### Service Configuration

#### LocalServiceConfig

```go
type LocalServiceConfig struct {
    ID       string // Unique identifier for the service
    Name     string // Display name (defaults to ID if empty)
    RootPath string // Root path for restricted browsing (empty = unrestricted)
}
```

**Notes:**
- If `RootPath` is empty, the service allows unrestricted browsing of the entire filesystem
- If `RootPath` is set, all operations are restricted to paths within that root
- Paths are automatically normalized and validated

#### SpectraServiceConfig

```go
type SpectraServiceConfig struct {
    ID         string // Unique identifier for the service
    Name       string // Display name (defaults to ID if empty)
    World      string // World name: "primary", "s1", "s2", etc. (defaults to "primary")
    RootID     string // Root node ID (defaults to "root")
    ConfigPath string // Path to Spectra configuration file (required)
}
```

**Notes:**
- `ConfigPath` must be an absolute path to a valid Spectra configuration file
- Multiple services can share the same `ConfigPath` but use different `World` values
- The root node must exist and be a folder type

## Error Handling

### Common Errors

- **`fs.ErrServiceNotFound`**: Returned when a service ID doesn't exist
- **Path validation errors**: Returned when trying to access paths outside a restricted root
- **Filesystem errors**: Wrapped errors from underlying filesystem operations

### Error Example

```go
def, err := manager.GetServiceDefinition("nonexistent")
if err == fs.ErrServiceNotFound {
    fmt.Println("Service not found")
} else if err != nil {
    log.Fatal(err)
}
```

## Thread Safety

The `ServiceManager` is thread-safe and can be used concurrently from multiple goroutines. All internal operations are protected by read-write mutexes.

Filesystem adapters (`LocalFS` and `SpectraFS`) are not thread-safe by themselves. If you need to use an adapter from multiple goroutines, you should:

1. Create separate adapter instances for each goroutine, or
2. Use appropriate synchronization (mutexes, channels, etc.)

## Best Practices

1. **Always release connections**: When using `AcquireAdapter`, always call the release function in a defer statement
2. **Use virtual services for flexibility**: Use "local" and "spectra" virtual service IDs when you don't need specific service configurations
3. **Handle pagination properly**: Check `HasMore` and adjust `Offset` to get all results
4. **Validate paths**: For local services with restricted roots, validate paths before operations
5. **Use context cancellation**: Pass contexts to long-running operations for cancellation support
6. **Worker owns the copy loop**: Use `io.Copy` or `io.CopyBuffer` in worker code, not in FS adapters
7. **Always close streams**: Always close `ReadCloser` and `WriteCloser` instances when done
8. **Close commits writes**: For `WriteCloser`, check the error returned by `Close()` - it finalizes the upload

## Performance Considerations

- **Connection pooling**: Reuse connection IDs for Spectra adapters to share SDK instances
- **Pagination**: Use appropriate page sizes (50-100 items) to balance memory and network usage
- **Path normalization**: Path normalization is done automatically but has minimal overhead
- **Concurrent access**: ServiceManager operations are safe for concurrent use
- **Streaming for large files**: adapters must stream on `Write` (no full-file temp spill); workers use a small copy buffer and beat progress on each chunk
- **Buffer size**: ME copy workers use a modest default buffer (tens of KiB). Prefer streaming adapters over huge staging buffers.
- **Spectra streaming**: SpectraFS `OpenWrite` accepts the byte stream without local staging (SDK does not persist upload bytes)

### Copy Buffer Configuration

The `fs` package provides a helper function for getting optimized copy buffers:

```go
import (
    "io"
    "codeberg.org/Sylos/Sylos-FS/pkg/fs"
)

// Use default 8MB buffer
buffer := fs.GetCopyBuffer(0)
defer func() {
    // Buffer can be reused or will be GC'd
}()
_, err := io.CopyBuffer(dstWriter, srcReader, buffer)

// Or specify custom size (e.g., 64MB for high-performance scenarios)
buffer := fs.GetCopyBuffer(64 * 1024 * 1024)
_, err := io.CopyBuffer(dstWriter, srcReader, buffer)
```

**Default buffer size**: 8MB (`fs.DefaultCopyBufferSize`)
- This is significantly larger than Go's default 32KB buffer
- Reduces system call overhead for large file transfers
- Good balance between memory usage and performance

**Custom buffer sizes**:
- Pass 0 or negative value to use default (8MB)
- Pass a positive value (in bytes) for custom size
- Common sizes: 8MB (default), 16MB, 32MB, 64MB
- Larger buffers reduce system calls but use more memory

## See Also

- `pkg/types` - Shared types and interfaces
- `codeberg.org/Sylos/Spectra/sdk` - Spectra filesystem SDK documentation

