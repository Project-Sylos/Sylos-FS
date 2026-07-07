// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

//go:build !windows

package local

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"codeberg.org/Sylos/Sylos-FS/pkg/types"
	"golang.org/x/sys/unix"
)

var linuxDriveFSTypes = map[string]struct{}{
	"ext4": {}, "ext3": {}, "ext2": {},
	"xfs": {}, "btrfs": {},
	"vfat": {}, "exfat": {}, "ntfs": {}, "ntfs3": {},
	"f2fs": {}, "reiserfs": {}, "jfs": {},
	"fuseblk": {}, // ntfs-3g and some exfat/fuse mounts on desktop Linux
}

// ListDrives enumerates local volumes and block devices for the current Unix-like OS.
func ListDrives() ([]types.DriveInfo, error) {
	switch runtime.GOOS {
	case "linux":
		return listLinuxDrives()
	case "darwin":
		return listDarwinDrives()
	default:
		return listFallbackDrives()
	}
}

func listLinuxDrives() ([]types.DriveInfo, error) {
	mounted, mountedSources, err := linuxMountedDrives()
	if err != nil {
		return nil, err
	}

	unmounted, err := linuxUnmountedPartitions(mountedSources)
	if err != nil {
		return nil, err
	}

	drives := append(mounted, unmounted...)
	sortDrives(drives)
	return drives, nil
}

func listDarwinDrives() ([]types.DriveInfo, error) {
	var drives []types.DriveInfo
	seen := make(map[string]struct{})

	if entries, err := os.ReadDir("/Volumes"); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := "/Volumes/" + entry.Name()
			if _, err := os.Stat(path); err != nil {
				continue
			}
			drive := types.DriveInfo{
				Path:        path,
				MountPoint:  path,
				Mounted:     true,
				DisplayName: entry.Name(),
				Type:        "fixed",
				Label:       entry.Name(),
			}
			applyFilesystemUsage(&drive, path)
			addDrive(&drives, seen, drive)
		}
	}

	if _, err := os.Stat("/"); err == nil {
		drive := types.DriveInfo{
			Path:        "/",
			MountPoint:  "/",
			Mounted:     true,
			DisplayName: "Local Disk (/)",
			Type:        "fixed",
		}
		applyFilesystemUsage(&drive, "/")
		addDrive(&drives, seen, drive)
	}

	sortDrives(drives)
	return drives, nil
}

func listFallbackDrives() ([]types.DriveInfo, error) {
	drive := types.DriveInfo{
		Path:        "/",
		MountPoint:  "/",
		Mounted:     true,
		DisplayName: "Local Disk (/)",
		Type:        "fixed",
	}
	applyFilesystemUsage(&drive, "/")
	return []types.DriveInfo{drive}, nil
}

func sortDrives(drives []types.DriveInfo) {
	sort.Slice(drives, func(i, j int) bool {
		if drives[i].Path == "/" {
			return true
		}
		if drives[j].Path == "/" {
			return false
		}
		return drives[i].Path < drives[j].Path
	})
}

func addDrive(drives *[]types.DriveInfo, seen map[string]struct{}, drive types.DriveInfo) {
	if drive.Path == "" {
		return
	}
	if _, exists := seen[drive.Path]; exists {
		return
	}
	seen[drive.Path] = struct{}{}
	*drives = append(*drives, drive)
}

func applyFilesystemUsage(d *types.DriveInfo, path string) {
	if d == nil {
		return
	}
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return
	}
	bsize := int64(st.Bsize)
	if bsize <= 0 {
		return
	}
	total := int64(st.Blocks) * bsize
	free := int64(st.Bavail) * bsize
	d.TotalBytes = total
	d.FreeBytes = free
	if total >= free {
		d.UsedBytes = total - free
	}
}

func linuxMountedDrives() ([]types.DriveInfo, map[string]struct{}, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	seen := make(map[string]struct{})
	mountedSources := make(map[string]struct{})
	var drives []types.DriveInfo

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		mountPoint, source, fstype, ok := parseMountinfoLine(scanner.Text())
		if !ok || !linuxIsBlockDeviceSource(source) {
			continue
		}
		if _, ok := linuxDriveFSTypes[fstype]; !ok {
			continue
		}
		if strings.HasPrefix(mountPoint, "/snap/") {
			continue
		}
		if linuxIsBootMount(mountPoint) {
			linuxRegisterMountedSource(mountedSources, source)
			continue
		}
		if _, exists := seen[mountPoint]; exists {
			continue
		}
		info, err := os.Stat(mountPoint)
		if err != nil || !info.IsDir() {
			continue
		}

		seen[mountPoint] = struct{}{}
		linuxRegisterMountedSource(mountedSources, source)
		drives = append(drives, linuxMountedDrive(mountPoint, source, fstype))
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return drives, mountedSources, nil
}

func linuxUnmountedPartitions(mountedSources map[string]struct{}) ([]types.DriveInfo, error) {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil, err
	}

	var drives []types.DriveInfo
	for _, entry := range entries {
		disk := entry.Name()
		if linuxSkipBlockDevice(disk) {
			continue
		}

		partitions, err := os.ReadDir(filepath.Join("/sys/block", disk))
		if err != nil {
			continue
		}

		foundPartition := false
		for _, partEntry := range partitions {
			if !partEntry.IsDir() || !linuxIsPartitionName(disk, partEntry.Name()) {
				continue
			}
			foundPartition = true
			drive, ok, err := linuxUnmountedPartitionDrive(disk, partEntry.Name(), mountedSources)
			if err != nil {
				return nil, err
			}
			if ok {
				drives = append(drives, drive)
			}
		}

		if !foundPartition {
			drive, ok, err := linuxUnmountedPartitionDrive(disk, disk, mountedSources)
			if err != nil {
				return nil, err
			}
			if ok {
				drives = append(drives, drive)
			}
		}
	}
	return drives, nil
}

func linuxUnmountedPartitionDrive(disk, part string, mountedSources map[string]struct{}) (types.DriveInfo, bool, error) {
	devicePath := filepath.Join("/dev", part)
	if linuxIsMountedDevice(devicePath, mountedSources) {
		return types.DriveInfo{}, false, nil
	}

	size, err := linuxBlockDeviceSize(filepath.Join("/sys/block", disk, part))
	if err != nil {
		size, err = linuxBlockDeviceSize(filepath.Join("/sys/block", disk))
		if err != nil {
			return types.DriveInfo{}, false, nil
		}
	}

	displayName := part
	if label := linuxDeviceLabel(devicePath); label != "" {
		displayName = label
	}

	return types.DriveInfo{
		Path:        devicePath,
		Device:      devicePath,
		Mounted:     false,
		DisplayName: displayName,
		Type:        linuxDriveType(devicePath),
		TotalBytes:  int64(size),
		Label:       linuxDeviceLabel(devicePath),
	}, true, nil
}

func linuxMountedDrive(mountPoint, source, fstype string) types.DriveInfo {
	drive := types.DriveInfo{
		Path:        mountPoint,
		MountPoint:  mountPoint,
		Device:      source,
		FileSystem:  fstype,
		Mounted:     true,
		DisplayName: linuxMountDisplayName(mountPoint, source),
		Type:        linuxDriveType(source),
		Label:       linuxDeviceLabel(source),
	}
	applyFilesystemUsage(&drive, mountPoint)
	return drive
}

func linuxDeviceLabel(device string) string {
	absDevice, err := filepath.EvalSymlinks(device)
	if err != nil {
		absDevice = device
	}

	entries, err := os.ReadDir("/dev/disk/by-label")
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		labelPath := filepath.Join("/dev/disk/by-label", entry.Name())
		target, err := filepath.EvalSymlinks(labelPath)
		if err != nil {
			continue
		}
		if target == absDevice {
			return linuxVolumeLabel(entry.Name())
		}
	}
	return ""
}

func linuxVolumeLabel(name string) string {
	name = unescapeMountinfo(name)
	return strings.ReplaceAll(name, `\x20`, " ")
}

func parseMountinfoLine(line string) (mountPoint, source, fstype string, ok bool) {
	sep := strings.Index(line, " - ")
	if sep < 0 {
		return "", "", "", false
	}

	before := strings.Fields(line[:sep])
	if len(before) < 5 {
		return "", "", "", false
	}
	mountPoint = unescapeMountinfo(before[4])

	after := strings.Fields(line[sep+3:])
	if len(after) < 2 {
		return "", "", "", false
	}
	return mountPoint, after[1], after[0], true
}

func unescapeMountinfo(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+3 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		val, err := strconv.ParseUint(s[i+1:i+4], 8, 8)
		if err != nil {
			b.WriteByte(s[i])
			continue
		}
		b.WriteByte(byte(val))
		i += 3
	}
	return b.String()
}

func linuxIsBootMount(mountPoint string) bool {
	return mountPoint == "/boot" || strings.HasPrefix(mountPoint, "/boot/") || mountPoint == "/efi"
}

func linuxIsBlockDeviceSource(source string) bool {
	if !strings.HasPrefix(source, "/dev/") {
		return false
	}
	base := strings.TrimPrefix(source, "/dev/")
	return !strings.HasPrefix(base, "loop") && !strings.HasPrefix(base, "ram")
}

func linuxSkipBlockDevice(name string) bool {
	switch {
	case strings.HasPrefix(name, "loop"):
		return true
	case strings.HasPrefix(name, "ram"):
		return true
	case strings.HasPrefix(name, "fd"):
		return true
	case strings.HasPrefix(name, "sr"):
		return true
	default:
		return false
	}
}

func linuxIsPartitionName(disk, part string) bool {
	if part == disk {
		return false
	}
	if strings.HasPrefix(part, disk) {
		return true
	}
	if strings.HasPrefix(disk, "nvme") && strings.HasPrefix(part, disk) && strings.Contains(part, "p") {
		return true
	}
	return false
}

func linuxBlockDeviceSize(sysPath string) (uint64, error) {
	data, err := os.ReadFile(filepath.Join(sysPath, "size"))
	if err != nil {
		return 0, err
	}
	sectors, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, err
	}
	return sectors * 512, nil
}

func linuxDriveType(source string) string {
	part := filepath.Base(source)
	disk := linuxParentDisk(part)
	if removable, err := os.ReadFile(filepath.Join("/sys/block", disk, "removable")); err == nil && strings.TrimSpace(string(removable)) == "1" {
		return "removable"
	}
	return "fixed"
}

func linuxParentDisk(part string) string {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return part
	}
	for _, entry := range entries {
		disk := entry.Name()
		if part == disk {
			return disk
		}
		if linuxIsPartitionName(disk, part) {
			return disk
		}
	}
	return part
}

func linuxMountDisplayName(mountPoint, source string) string {
	if mountPoint == "/" {
		return fmt.Sprintf("Local Disk (/) — %s", filepath.Base(source))
	}
	return fmt.Sprintf("%s — %s", mountPoint, filepath.Base(source))
}

func linuxRegisterMountedSource(mountedSources map[string]struct{}, source string) {
	if source == "" {
		return
	}
	mountedSources[source] = struct{}{}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil || resolved == "" || resolved == source {
		return
	}
	mountedSources[resolved] = struct{}{}
}

func linuxIsMountedDevice(devicePath string, mountedSources map[string]struct{}) bool {
	if _, mounted := mountedSources[devicePath]; mounted {
		return true
	}
	resolved, err := filepath.EvalSymlinks(devicePath)
	if err != nil {
		return false
	}
	if resolved != devicePath {
		if _, mounted := mountedSources[resolved]; mounted {
			return true
		}
	}
	return false
}
