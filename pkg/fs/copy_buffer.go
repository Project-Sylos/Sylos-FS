// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package fs

// DefaultCopyBufferSize is the default buffer size used for io.CopyBuffer operations.
// This is set to 8MB, which is a good balance between memory usage and performance
// for most file copy operations. Industry standard is typically 1-64MB depending on
// use case, with 8MB being a common choice for general-purpose file operations.
const DefaultCopyBufferSize = 8 * 1024 * 1024 // 8MB

// GetCopyBuffer returns a buffer suitable for use with io.CopyBuffer.
// If bufferSize is 0 or negative, it uses DefaultCopyBufferSize (8MB).
// Otherwise, it uses the provided bufferSize.
//
// Example usage:
//
//	buffer := fs.GetCopyBuffer(0) // Uses default 8MB
//	defer bufferPool.Put(buffer)
//	_, err := io.CopyBuffer(dstWriter, srcReader, buffer)
//
// Or with custom size:
//
//	buffer := fs.GetCopyBuffer(64 * 1024 * 1024) // 64MB
//	defer bufferPool.Put(buffer)
//	_, err := io.CopyBuffer(dstWriter, srcReader, buffer)
func GetCopyBuffer(bufferSize int) []byte {
	if bufferSize <= 0 {
		bufferSize = DefaultCopyBufferSize
	}
	return make([]byte, bufferSize)
}
