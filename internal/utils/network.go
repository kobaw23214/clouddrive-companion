package utils

import (
	"log"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type SystemStats struct {
	NumGoroutines  int     `json:"numGoroutines"`
	AllocMem       uint64  `json:"allocMem"` // 已分配的内存(HeapAlloc)
	SysMem         uint64  `json:"sysMem"`   // 进程获取的总内存
	TcpTimeWait    int     `json:"tcpTimeWait"`
	TcpEstablished int     `json:"tcpEstablished"`
	PortUsage      float64 `json:"portUsage"`
	MaxPorts       int     `json:"maxPorts"` // 系统可分配的端口容量
	IsWarning      bool    `json:"isWarning"`
}

// 缓存系统端口容量，避免每次都执行 netsh 命令
var (
	cachedMaxPorts     int
	maxPortsOnce       sync.Once
	defaultMaxPorts    = 16384 // Windows 默认动态端口范围 49152-65535
)

// getDynamicPortRange 获取系统动态端口范围
// 通过执行 netsh int ipv4 show dynamicport tcp 命令获取
func getDynamicPortRange() int {
	maxPortsOnce.Do(func() {
		cmd := exec.Command("netsh", "int", "ipv4", "show", "dynamicport", "tcp")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

		output, err := cmd.Output()
		if err != nil {
			log.Printf("[NetworkStats] 获取动态端口范围失败: %v，使用默认值 %d", err, defaultMaxPorts)
			cachedMaxPorts = defaultMaxPorts
			return
		}

		// 解析输出，格式类似：
		// 协议 tcp 动态端口范围
		// ---------------------------------
		// 启动端口        : 49152
		// 端口数          : 16384
		//
		// 或英文：
		// Protocol tcp Dynamic Port Range
		// ---------------------------------
		// Start Port      : 49152
		// Number of Ports : 16384
		outputStr := string(output)
		
		// 使用正则匹配数字（端口数那一行）
		// 匹配 "端口数" 或 "Number of Ports" 后面的数字
		re := regexp.MustCompile(`(?:端口数|Number of Ports)\s*:\s*(\d+)`)
		matches := re.FindStringSubmatch(outputStr)
		
		if len(matches) >= 2 {
			if ports, err := strconv.Atoi(matches[1]); err == nil && ports > 0 {
				cachedMaxPorts = ports
				log.Printf("[NetworkStats] 检测到系统动态端口数量: %d", cachedMaxPorts)
				return
			}
		}

		log.Printf("[NetworkStats] 解析动态端口范围失败，使用默认值 %d", defaultMaxPorts)
		cachedMaxPorts = defaultMaxPorts
	})

	return cachedMaxPorts
}

// GetNetworkStats 获取网络状态，并隐藏窗口
func GetNetworkStats() SystemStats {
	var stats SystemStats
	stats.NumGoroutines = runtime.NumGoroutine()
	
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	stats.AllocMem = m.Alloc
	stats.SysMem = m.Sys
	
	// 动态获取系统端口容量
	maxPorts := getDynamicPortRange()
	stats.MaxPorts = maxPorts

	// 执行 netstat
	cmd := exec.Command("netstat", "-an")
	// 隐藏 CMD 窗口，防止闪烁
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.Output()
	if err != nil {
		log.Printf("[NetworkStats] 执行 netstat 失败: %v", err)
		return stats
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "TIME_WAIT") {
			stats.TcpTimeWait++
		} else if strings.Contains(line, "ESTABLISHED") {
			stats.TcpEstablished++
		}
	}

	totalUsed := float64(stats.TcpTimeWait + stats.TcpEstablished)
	stats.PortUsage = (totalUsed / float64(maxPorts)) * 100
	if stats.PortUsage > 100 {
		stats.PortUsage = 100
	}

	if stats.TcpTimeWait > 4000 || stats.PortUsage > 80 {
		stats.IsWarning = true
	}

	if stats.IsWarning {
		log.Printf("[NetworkStats] Goroutines: %d | ESTABLISHED: %d | TIME_WAIT: %d | MaxPorts: %d | Usage: %.2f%% | Warning: %v",
			stats.NumGoroutines, stats.TcpEstablished, stats.TcpTimeWait, maxPorts, stats.PortUsage, stats.IsWarning)
	}

	return stats
}
