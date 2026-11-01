//go:build !windows

package app

import "os"

// 非 Windows 平台的桩实现。文件选择器面向 Windows 现场使用，
// 这里只保证 `go build` 在其他平台仍能通过（config 包有 Linux/Fallback 分支）。

// shellKnownFolders 无 Shell 已知文件夹概念，返回 nil 让调用方走 HOME 拼接回退。
func shellKnownFolders() []knownFolder { return nil }

// isHiddenSystemEntry 非 Windows 没有 HIDDEN+SYSTEM 属性组合，一律不隐藏。
func isHiddenSystemEntry(info os.FileInfo) bool { return false }

// describeVolume 非 Windows 不做卷探测。
func describeVolume(path string) string { return "volume=n/a" }
