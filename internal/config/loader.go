package config

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	AppConfig     Config
	LogFile       *os.File
	AppSupportDir string
	MD5Cache      = &sync.Map{}

	// configMu 保护 config.json 的读写、AppConfig 的可变字段（map / 指针指向的值）
	configMu sync.Mutex
)

type syncLogger struct {
	file *os.File
	mu   sync.Mutex
}

func (s *syncLogger) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err = s.file.Write(p)
	if err != nil {
		return n, err
	}
	err = s.file.Sync()
	return n, err
}

// SetupAppSupportDir 负责检测操作系统并设置一个安全的数据写入路径
func SetupAppSupportDir() {
	switch runtime.GOOS {
	case "linux":
		userConfigDir, err := os.UserConfigDir()
		if err != nil {
			log.Fatalf("严重错误 (Linux): 无法找到用户配置目录: %v", err)
			os.Exit(1)
		}
		AppSupportDir = filepath.Join(userConfigDir, "clouddrive-companion")
		log.Println("检测到 Linux, 配置文件目录:", AppSupportDir)

	case "windows":
		exePath, err := os.Executable()
		if err != nil {
			log.Fatalf("严重错误 (Windows): 无法获取程序路径: %v", err)
			os.Exit(1)
		}
		AppSupportDir = filepath.Dir(exePath)
		log.Println("检测到 Windows, 配置文件目录:", AppSupportDir)

	default:
		userConfigDir, err := os.UserConfigDir()
		if err != nil {
			log.Fatalf("严重错误 (Fallback): 无法找到用户配置目录: %v", err)
			os.Exit(1)
		}
		AppSupportDir = filepath.Join(userConfigDir, "clouddrive-companion")
		log.Println("检测到 Fallback OS, 配置文件目录:", AppSupportDir)
	}

	if err := os.MkdirAll(AppSupportDir, 0755); err != nil {
		log.Fatalf("严重错误: 无法创建应用支持目录 %s: %v", AppSupportDir, err)
		os.Exit(1)
	}
}

// SetupLogging 初始化日志系统
func SetupLogging() {
	var err error

	logDir := AppSupportDir
	logFileName := fmt.Sprintf("clouddrive-companion-%s.log", time.Now().Format("2006-01-02"))
	logFilePath := filepath.Join(logDir, logFileName)

	LogFile, err = os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		initialError := err
		userConfigDir, err := os.UserConfigDir()
		if err != nil {
			log.Fatalf("Fatal Error creating user config dir: %v\nInitial error: %v", err, initialError)
			os.Exit(1)
		}
		appLogDir := filepath.Join(userConfigDir, "MyCloudDriveCompanion")
		if err := os.MkdirAll(appLogDir, 0755); err != nil {
			log.Fatalf("Fatal Error creating app log dir: %v", err)
			os.Exit(1)
		}
		logFilePath = filepath.Join(appLogDir, logFileName)
		LogFile, err = os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Fatalf("Fatal Error opening log file in user dir: %v\nInitial error: %v", err, initialError)
			os.Exit(1)
		}
	}
	logger := &syncLogger{file: LogFile}
	log.SetOutput(logger)
	log.Println("------------------------------------")
	log.Println("Application starting up...")
	log.Println("Log file is at:", logFilePath)
}

// LoadConfig 加载配置文件
func LoadConfig() {
	configPath := filepath.Join(AppSupportDir, "config.json")
	log.Printf("正在从 %s 加载配置...", configPath)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Println("配置文件不存在,将创建一个默认的 config.json。")

		configData, err := json.MarshalIndent(DefaultConfig, "", "  ")
		if err != nil {
			log.Fatalf("严重错误: 无法生成默认配置: %v", err)
			os.Exit(1)
		}
		if err := os.WriteFile(configPath, configData, 0666); err != nil {
			log.Fatalf("严重错误: 无法写入默认配置文件: %v", err)
			os.Exit(1)
		}
		log.Println("默认配置文件已成功创建。")
	}

	file, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("严重错误: 无法读取配置文件: %v", err)
		os.Exit(1)
	}

	if err := json.Unmarshal(file, &AppConfig); err != nil {
		log.Fatalf("严重错误: 配置文件格式错误 (非法的JSON): %v", err)
		os.Exit(1)
	}

	// 二次扫描原始 JSON，区分"字段缺失"与"显式设置为 zero value"
	var rawFields map[string]json.RawMessage
	_ = json.Unmarshal(file, &rawFields)
	if _, present := rawFields["traceEnabled"]; !present {
		AppConfig.TraceEnabled = true // 老 config.json 升级时默认开
	}
	if _, present := rawFields["outboundHttpLog"]; !present {
		AppConfig.OutboundHttpLog = true // 老 config.json 升级时默认开
	}
	if _, present := rawFields["outboundHttpLogPoller"]; !present {
		AppConfig.OutboundHttpLogPoller = true // 老 config.json 升级时默认开
	}

	if AppConfig.SendRelativePath == nil {
		defaultValue := true
		AppConfig.SendRelativePath = &defaultValue
	}
	if AppConfig.SendFileSize == nil {
		defaultValue := true
		AppConfig.SendFileSize = &defaultValue
	}
	if AppConfig.MergeTimeoutMultiplier <= 0 {
		AppConfig.MergeTimeoutMultiplier = 20
	}

	// 链路追踪：补默认
	if AppConfig.TraceModuleEnabled == nil {
		AppConfig.TraceModuleEnabled = map[string]bool{}
	}
	if AppConfig.TraceSamplingThreshold == nil {
		v := defaultTraceSamplingThreshold
		AppConfig.TraceSamplingThreshold = &v
	}
	if AppConfig.TraceMaxAgeDays == nil {
		v := defaultTraceMaxAgeDays
		AppConfig.TraceMaxAgeDays = &v
	}
	if AppConfig.TraceMaxRows == nil {
		v := defaultTraceMaxRows
		AppConfig.TraceMaxRows = &v
	}
	if AppConfig.TraceVacuumThresholdMB == nil {
		v := defaultTraceVacuumThresholdMB
		AppConfig.TraceVacuumThresholdMB = &v
	}

	AppConfig.BackendUploadURL = strings.TrimSpace(AppConfig.BackendUploadURL)
	AppConfig.BackendCompleteURL = strings.TrimSpace(AppConfig.BackendCompleteURL)
	if AppConfig.MaxWorkers <= 0 {
		AppConfig.MaxWorkers = runtime.NumCPU()
	}
	log.Println("配置加载成功。")
	log.Printf("上传 URL: %s", AppConfig.BackendUploadURL)
	log.Printf("命令 URL: %s", AppConfig.BackendCommandURL)
	log.Printf("最大并发数: %d", AppConfig.MaxWorkers)
	log.Printf("发送相对路径: %t", *AppConfig.SendRelativePath)
	log.Printf("发送文件大小: %t", *AppConfig.SendFileSize)
	log.Printf("标准超时: %d s", AppConfig.HTTPTimeout)
	log.Printf("合并超时乘数: %d (最终合并超时: %d s)", AppConfig.MergeTimeoutMultiplier, AppConfig.HTTPTimeout*AppConfig.MergeTimeoutMultiplier)
}

// InitHTTPClient 初始化全局 HTTP 客户端
func InitHTTPClient() *http.Client {
	maxConns := AppConfig.MaxWorkers + 50
	if maxConns < 100 {
		maxConns = 100
	}

	client := &http.Client{
		Timeout: time.Duration(AppConfig.HTTPTimeout) * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          maxConns * 2,
			MaxIdleConnsPerHost:   maxConns,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	log.Printf("全局 HTTP Client 已初始化 (MaxIdleConnsPerHost: %d)", maxConns)
	return client
}


// Save 把当前 AppConfig 序列化写回到 <AppSupportDir>/config.json
//
// 实现要点：
//   - 加锁防止并发写竞态（同时也防止 LoadConfig 与 Save 交错）
//   - 先写到 config.json.tmp，再用 os.Rename 原子替换，避免崩溃时配置半截损坏
//   - 写入失败时清理 .tmp 残留
func Save() error {
	configMu.Lock()
	defer configMu.Unlock()

	if AppSupportDir == "" {
		return fmt.Errorf("config: AppSupportDir 未初始化")
	}

	data, err := json.MarshalIndent(&AppConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal 失败: %w", err)
	}

	configPath := filepath.Join(AppSupportDir, "config.json")
	tmpPath := configPath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("config: 写入临时文件失败: %w", err)
	}

	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("config: 替换配置文件失败: %w", err)
	}
	return nil
}
