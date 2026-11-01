package config

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
)

// ──────────────────────────────────────────────────────────────────────────
// 运行时后端地址覆盖（in-memory backend override）
//
// 背景：
//   后端服务是多地部署的，同一套客户端可能要连不同机房的 IP/端口。原来的做法
//   是手动改安装目录下的 config.json。现在新增一种"前端拉起客户端时把服务器
//   地址带过来"的方式，优先级高于 config.json，且只保存在内存里——进程退出即
//   失效，不写盘、不污染配置文件。
//
// 覆盖范围：
//   只覆盖后端四个地址（chunk/complete/command/check）的 scheme + host:port，
//   路径部分（如 /dataImport/upload/chunk）仍取自 config.json。因为多地部署
//   通常只是 IP/端口不同，接口路径是一致的。
//
// 优先级：
//   URI 传入的 server 参数  >  config.json 中的完整 URL
//   没传 server 时，BackendXxxURL() 原样返回 config.json 的值（兼容旧版本）。
//
// 来源：
//   main.go 在解析 my-clouddrive://select-files / open-client 的 URI 时，读取
//   query 参数 server=，调用 SetBackendOverride() 写入。
// ──────────────────────────────────────────────────────────────────────────

var (
	overrideMu sync.RWMutex
	// backendOverrideBase 形如 "http://192.168.1.100:8081"，空串表示未覆盖。
	backendOverrideBase string
)

// SetBackendOverride 设置后端地址的内存覆盖。
//
// server 可接受以下形式：
//   - "http://192.168.1.100:8081"
//   - "https://192.168.1.100:8081"
//   - "192.168.1.100:8081"        （缺省补 http://）
//
// 仅保留 scheme + host:port，忽略传入的任何 path/query。空串视为"不覆盖"
// （清除已有覆盖）。解析失败返回 error，调用方可记录日志后继续用 config.json。
func SetBackendOverride(server string) error {
	server = strings.TrimSpace(server)
	if server == "" {
		overrideMu.Lock()
		backendOverrideBase = ""
		overrideMu.Unlock()
		return nil
	}

	// 没带 scheme 时补一个，方便前端只传 "ip:port"
	if !strings.Contains(server, "://") {
		server = "http://" + server
	}

	u, err := url.Parse(server)
	if err != nil {
		return fmt.Errorf("无法解析 server 覆盖地址 %q: %w", server, err)
	}
	if u.Host == "" {
		return fmt.Errorf("server 覆盖地址 %q 缺少 host", server)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("server 覆盖地址 %q 的协议必须是 http 或 https", server)
	}

	base := u.Scheme + "://" + u.Host

	overrideMu.Lock()
	backendOverrideBase = base
	overrideMu.Unlock()

	log.Printf("✅ 已启用后端地址内存覆盖（优先于 config.json）: %s", base)
	return nil
}

// GetBackendOverrideBase 返回当前内存覆盖基址（scheme://host:port），未覆盖时返回空串。
func GetBackendOverrideBase() string {
	overrideMu.RLock()
	defer overrideMu.RUnlock()
	return backendOverrideBase
}

// applyBackendOverride 把 rawURL 的 scheme + host 替换为覆盖基址，保留其 path/query。
// 未设置覆盖时原样返回。解析失败时退回原值，保证永不返回空串。
func applyBackendOverride(rawURL string) string {
	overrideMu.RLock()
	base := backendOverrideBase
	overrideMu.RUnlock()

	if base == "" {
		return rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		log.Printf("[override] 解析配置 URL 失败，退回原值: %q (%v)", rawURL, err)
		return rawURL
	}
	b, err := url.Parse(base)
	if err != nil {
		return rawURL
	}
	u.Scheme = b.Scheme
	u.Host = b.Host
	return u.String()
}

// ── 后端地址读取入口 ──
//
// 业务代码（api / uploader）一律通过下面四个函数取后端地址，不要再直接读
// AppConfig.BackendXxxURL。这样内存覆盖才能统一生效。
//
// 注意：配置编辑器（app.GetConfig / config.GetFieldsMeta）仍读 AppConfig 原始
// 值，因为编辑器要展示并保存 config.json 的真实内容，不能被运行时覆盖影响。

// BackendUploadURL 分片上传地址（应用覆盖后）
func BackendUploadURL() string { return applyBackendOverride(AppConfig.BackendUploadURL) }

// BackendCompleteURL 上传完成通知地址（应用覆盖后）
func BackendCompleteURL() string { return applyBackendOverride(AppConfig.BackendCompleteURL) }

// BackendCommandURL 命令轮询地址（应用覆盖后）
func BackendCommandURL() string { return applyBackendOverride(AppConfig.BackendCommandURL) }

// BackendCheckURL 分片校验地址（应用覆盖后）
func BackendCheckURL() string { return applyBackendOverride(AppConfig.BackendCheckURL) }

// EffectiveBackendBase 返回当前实际生效的后端基址（scheme://host[:port]），
// 供界面展示"当前连接的后端"用。
//
//	有内存覆盖   → 返回覆盖基址
//	无内存覆盖   → 从 config.json 的上传地址解析出 scheme://host
//	都拿不到     → 返回空串（调用方自行处理）
func EffectiveBackendBase() string {
	if base := GetBackendOverrideBase(); base != "" {
		return base
	}
	u, err := url.Parse(strings.TrimSpace(AppConfig.BackendUploadURL))
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	return scheme + "://" + u.Host
}
