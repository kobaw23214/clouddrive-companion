//go:build windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// Win32 GetDriveType 的返回值。x/sys/windows 没有导出这些常量，按 Win32 定义就地声明。
const (
	driveUnknown   uint32 = 0
	driveNoRootDir uint32 = 1
	driveRemovable uint32 = 2
	driveFixed     uint32 = 3
	driveRemote    uint32 = 4
	driveCDROM     uint32 = 5
	driveRamdisk   uint32 = 6
)

// shellKnownFolders 通过 SHGetKnownFolderPath 解析「桌面 / 下载 / 文档」的真实路径。
//
// 为什么不能拼 %USERPROFILE%\Desktop：这三个目录都可能被重定向
//   - OneDrive 已知文件夹保护（KFM）→ %USERPROFILE%\OneDrive\桌面
//   - 企业 GPO 文件夹重定向        → \\server\share\<user>\Desktop
//
// 重定向之后 %USERPROFILE%\Desktop 往往还留着一个空壳目录。手工拼路径会指到空壳上，
// 表现就是：用户在系统原生文件对话框里明明能看到刚放进去的文件夹，在本选择器里
// 却永远看不到，而且刷新多少次都没用（刷的是另一个目录）。
// Shell API 返回的是重定向后的真实路径，与原生对话框完全一致。
func shellKnownFolders() []knownFolder {
	specs := []struct {
		name string
		id   *windows.KNOWNFOLDERID
	}{
		{"桌面", windows.FOLDERID_Desktop},
		{"下载", windows.FOLDERID_Downloads},
		{"文档", windows.FOLDERID_Documents},
	}

	result := make([]knownFolder, 0, len(specs))
	for _, spec := range specs {
		path, err := windows.KnownFolderPath(spec.id, windows.KF_FLAG_DEFAULT)
		result = append(result, knownFolder{Name: spec.name, Path: path, Err: err})
	}
	return result
}

// isHiddenSystemEntry 判断条目是否同时带 HIDDEN + SYSTEM 属性，
// 即资源管理器默认归类为「受保护的操作系统文件」的那一类。
//
// 用属性判定取代原先的「名字以 . 或 $ 开头就跳过」：后者会把用户自己建的
// .xxx / $xxx 目录一并藏掉，而这类目录在原生对话框里是可见的，属于误伤。
func isHiddenSystemEntry(info os.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false
	}
	const mask = syscall.FILE_ATTRIBUTE_HIDDEN | syscall.FILE_ATTRIBUTE_SYSTEM
	return data.FileAttributes&mask == mask
}

// describeVolume 返回路径所在卷的类型与文件系统名，用于现场排障。
//
// 这是区分「本地 NTFS」和「网络盘 / 挂载的虚拟盘」的关键信息：本地 NTFS 的目录
// 枚举没有客户端缓存，不可能读到陈旧内容；而 SMB 映射盘（driveType=REMOTE）和
// 网盘挂载驱动（fs 不是 NTFS/FAT 而是自定义名）都有自己的元数据缓存，
// 那种情况下应用层怎么刷新都没用，得从挂载配置入手。
func describeVolume(path string) string {
	root := filepath.VolumeName(path)
	if root == "" {
		return "volume=?"
	}
	if !strings.HasSuffix(root, `\`) {
		root += `\`
	}

	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "volume=" + root
	}

	var driveType string
	switch windows.GetDriveType(rootPtr) {
	case driveRemovable:
		driveType = "REMOVABLE"
	case driveFixed:
		driveType = "FIXED"
	case driveRemote:
		driveType = "REMOTE(网络/映射盘)"
	case driveCDROM:
		driveType = "CDROM"
	case driveRamdisk:
		driveType = "RAMDISK"
	case driveNoRootDir:
		driveType = "NO_ROOT_DIR"
	case driveUnknown:
		driveType = "UNKNOWN"
	default:
		driveType = "?"
	}

	fsName := "?"
	var fsBuf [windows.MAX_PATH + 1]uint16
	var labelBuf [windows.MAX_PATH + 1]uint16
	var serial, maxComponentLen, flags uint32
	if err := windows.GetVolumeInformation(rootPtr,
		&labelBuf[0], uint32(len(labelBuf)),
		&serial, &maxComponentLen, &flags,
		&fsBuf[0], uint32(len(fsBuf))); err == nil {
		fsName = windows.UTF16ToString(fsBuf[:])
	}

	return fmt.Sprintf("volume=%s driveType=%s fs=%s", root, driveType, fsName)
}
