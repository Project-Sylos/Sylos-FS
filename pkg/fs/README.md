# fs Package

The `fs` package provides filesystem adapters and service management for the Sylos project. It implements a unified interface for interacting with different filesystem types, including local filesystems and the Spectra filesystem simulator.

## Overview

This package contains:

- **LocalFS**: Adapter for local operating system filesystem
- **SpectraFS**: Adapter for Spectra filesystem simulator
- **ServiceManager**: Centralized service configuration and lifecycle management

All adapters implement the `types.FSAdapter` interface, providing a consistent API regardless of the underlying storage backend.

## Filesystem Adapters

### LocalFS

The `LocalFS` adapter provides access to the local operating system filesystem. It handles path normalization, cross-platform compatibility, and root-relative path tracking.

#### Features

- **Cross-platform support**: Works on Windows and Unix-like systems
- **Path normalization**: Automatically normalizes paths to use forward slashes
- **Root-relative paths**: Maintains logical paths relative to a configured root
- **Unrestricted browsing**: Can be configured to allow browsing outside the root path

#### Example

```go
import (
    "github.com/Project-Sylos/Sylos-FS/pkg/fs"
    "github.com/Project-Sylos/Sylos-FS/pkg/types"
)

// Create a local filesystem adapter rooted at /home/user
localFS, err := fs.NewLocalFS("/home/user")
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

// Download a file
reader, err := localFS.DownloadFile("/home/user/documents/file.txt")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

// Create a new folder
folder, err := localFS.CreateFolder("/home/user/documents", "new-folder")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Created folder: %s\n", folder.LocationPath)

// Upload a file
content := strings.NewReader("file content here")
file, err := localFS.UploadFile("/home/user/documents/new-file.txt", content)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Uploaded file: %s (%d bytes)\n", file.DisplayName, file.Size)
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
    "github.com/Project-Sylos/Sylos-FS/pkg/fs"
    "github.com/Project-Sylos/Spectra/sdk"
)

// Create a Spectra SDK instance
spectraSDK, err := sdk.New("/path/to/spectra/config")
if err != nil {
    log.Fatal(err)
}
defer spectraSDK.Close()

// Create a SpectraFS adapter
spectraFS, err := fs.NewSpectraFS(spectraSDK, "root", "primary")
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

// Upload a file to the folder
content := strings.NewReader("file content")
file, err := spectraFS.UploadFile(folder.Id, content)
if err != nil {
    log.Fatal(err)
}
```

#### Worlds

Spectra supports multiple "worlds" which are separate data contexts:

- **primary**: The main world, typically used as the source
- **s1, s2, etc.**: Secondary worlds, typically used as destinations

When creating a `SpectraFS` adapter, you specify which world to operate in. All operations (list, create, upload) will be scoped to that world.

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
    "github.com/Project-Sylos/Sylos-FS/pkg/fs"
    "github.com/Project-Sylos/Sylos-FS/pkg/types"
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

## Performance Considerations

- **Connection pooling**: Reuse connection IDs for Spectra adapters to share SDK instances
- **Pagination**: Use appropriate page sizes (50-100 items) to balance memory and network usage
- **Path normalization**: Path normalization is done automatically but has minimal overhead
- **Concurrent access**: ServiceManager operations are safe for concurrent use

## See Also

- `pkg/types` - Shared types and interfaces
- `github.com/Project-Sylos/Spectra/sdk` - Spectra filesystem SDK documentation

