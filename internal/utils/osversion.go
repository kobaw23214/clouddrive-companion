package utils

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// Windows 版本判定工具。
//
// 为什么用 RtlGetVersion 而不是 GetVersionEx / windows.GetVersion：
//   GetVersionEx 受应用清单（wails.exe.manifest）的兼容性垫片影响，在没有声明
//   supportedOS 的程序里可能谎报为 6.2（Win8）。RtlGetVersion 直接读内核版本，
//   不受垫片影响，是判断"真实系统版本"的可靠方式。
//
// 这些判定当前用于 WebView2 运行时选择：
//   Win7/8/8.1 上 WebView2 被微软永久冻结在 Chromium 109（2023-01 封版），
//   而 Win10+ 可以使用最新版本，两者需要不同的运行时目录。

// WindowsVersion 返回内核报告的主版本号、次版本号和 build 号。
func WindowsVersion() (major, minor, build uint32) {
	v := windows.RtlGetVersion()
	return v.MajorVersion, v.MinorVersion, v.BuildNumber
}

// WindowsVersionString 返回形如 "6.1.7601" 的版本串，便于打日志。
func WindowsVersionString() string {
	major, minor, build := WindowsVersion()
	return fmt.Sprintf("%d.%d.%d", major, minor, build)
}

// IsWindows10OrLater 判断是否为 Windows 10 及更高版本（含 Windows 11）。
//
// Windows 11 的内核主版本号仍是 10，靠 build 号（>= 22000）区分，
// 所以这里只需判断 major >= 10。
func IsWindows10OrLater() bool {
	major, _, _ := WindowsVersion()
	return major >= 10
}

// IsLegacyWindows 判断是否为 Windows 7 / 8 / 8.1 及对应的 Server 版本
// （内核版本 6.1 / 6.2 / 6.3）。这些系统上 WebView2 的最高可用版本是 109。
func IsLegacyWindows() bool {
	major, minor, _ := WindowsVersion()
	if major > 6 {
		return false
	}
	// 6.1 = Win7 / Server 2008 R2
	// 6.2 = Win8 / Server 2012
	// 6.3 = Win8.1 / Server 2012 R2
	return major == 6 && minor >= 1
}

// WindowsFriendlyName 返回便于阅读的系统名称，仅用于日志输出。
func WindowsFriendlyName() string {
	major, minor, build := WindowsVersion()
	switch {
	case major == 6 && minor == 1:
		return "Windows 7 / Server 2008 R2"
	case major == 6 && minor == 2:
		return "Windows 8 / Server 2012"
	case major == 6 && minor == 3:
		return "Windows 8.1 / Server 2012 R2"
	case major == 10 && build >= 22000:
		return "Windows 11"
	case major == 10:
		return "Windows 10 / Server 2016+"
	default:
		return "Windows " + WindowsVersionString()
	}
}
