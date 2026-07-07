// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build !windows

package local

import (
	"runtime"
	"testing"
)

func TestParseMountinfoLine(t *testing.T) {
	t.Parallel()

	mountPoint, source, fstype, ok := parseMountinfoLine(
		"519 518 8:2 / / rw,relatime shared:1 - ext4 /dev/sda2 rw",
	)
	if !ok {
		t.Fatal("expected parse success")
	}
	if mountPoint != "/" || source != "/dev/sda2" || fstype != "ext4" {
		t.Fatalf("got mount=%q source=%q fstype=%q", mountPoint, source, fstype)
	}

	mountPoint, source, fstype, ok = parseMountinfoLine(
		"36 35 98:0 /mnt1 /mnt\\040data rw,noatime master:1 - ext3 /dev/sdb1 rw,errors=continue",
	)
	if !ok {
		t.Fatal("expected escaped mount point parse success")
	}
	if mountPoint != "/mnt data" || source != "/dev/sdb1" || fstype != "ext3" {
		t.Fatalf("got mount=%q source=%q fstype=%q", mountPoint, source, fstype)
	}
}

func TestLinuxIsBootMount(t *testing.T) {
	t.Parallel()

	if !linuxIsBootMount("/boot/efi") || !linuxIsBootMount("/boot") {
		t.Fatal("expected boot mounts to be detected")
	}
	if linuxIsBootMount("/") || linuxIsBootMount("/mnt/data") {
		t.Fatal("expected non-boot mounts to pass through")
	}
}

func TestLinuxIsBlockDeviceSource(t *testing.T) {
	t.Parallel()

	if !linuxIsBlockDeviceSource("/dev/sda2") {
		t.Fatal("expected sda2 to be a block device source")
	}
	if linuxIsBlockDeviceSource("/dev/loop0") {
		t.Fatal("expected loop device to be excluded")
	}
	if linuxIsBlockDeviceSource("tmpfs") {
		t.Fatal("expected non-/dev source to be excluded")
	}
}

func TestLinuxIsMountedDeviceResolvesSymlinks(t *testing.T) {
	t.Parallel()

	mountedSources := map[string]struct{}{"/dev/disk/by-uuid/example": {}}
	linuxRegisterMountedSource(mountedSources, "/dev/disk/by-uuid/example")

	if !linuxIsMountedDevice("/dev/disk/by-uuid/example", mountedSources) {
		t.Fatal("expected direct source path to be mounted")
	}
}

func TestListDrivesLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific drive listing")
	}

	drives, err := ListDrives()
	if err != nil {
		t.Fatalf("ListDrives: %v", err)
	}
	if len(drives) == 0 {
		t.Fatal("expected at least one drive on linux")
	}

	foundRoot := false
	for _, drive := range drives {
		if drive.Path == "/" {
			foundRoot = true
			if drive.TotalBytes <= 0 {
				t.Fatalf("expected root drive totalBytes > 0: %+v", drive)
			}
			if drive.FreeBytes < 0 {
				t.Fatalf("expected root drive freeBytes >= 0: %+v", drive)
			}
			if !drive.Mounted {
				t.Fatalf("expected root drive to be mounted: %+v", drive)
			}
			if drive.Device == "" || drive.FileSystem == "" {
				t.Fatalf("expected root drive device and filesystem: %+v", drive)
			}
		}
		if drive.Path == "" {
			t.Fatalf("drive path must not be empty: %+v", drive)
		}
	}
	if !foundRoot {
		t.Fatalf("expected root mount in drives: %+v", drives)
	}
}
