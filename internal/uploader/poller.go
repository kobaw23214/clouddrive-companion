package uploader

import (
	"clouddrive-companion-withWails2/internal/api"
	"clouddrive-companion-withWails2/internal/config"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CommandPoller 命令轮询器接口
type CommandPoller interface {
	TriggerStatsRefresh()
}

// PollCommands 轮询命令
func PollCommands(m *Manager, poller CommandPoller) {
	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()

	cmdURL := fmt.Sprintf("%s/%s", config.BackendCommandURL(), m.taskId)

	for {
		select {
		case <-m.ctx.Done():
			log.Printf("任务 %s 结束，停止轮询。", m.taskId)
			return

		case <-ticker.C:
			req, err := http.NewRequest("GET", cmdURL, nil)
			if err != nil {
				log.Printf("[Poller] 创建请求失败: %v", err)
				continue
			}
			pollCtx, cancel := context.WithTimeout(m.ctx, time.Duration(config.AppConfig.HTTPTimeout)*time.Second)
			req = req.WithContext(api.ContextWithPollerMark(pollCtx))
			resp, err := api.GlobalHTTPClient.Do(req)
			cancel()

			if err != nil {
				if err != context.Canceled {
					log.Printf("[Poller] 请求命令失败: %v", err)
				}
				continue
			}
			if resp.StatusCode != http.StatusOK {
				log.Printf("[Poller] 服务器返回错误: %s", resp.Status)
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				continue
			}
			var cmdResp CommandResponse
			if err := json.NewDecoder(resp.Body).Decode(&cmdResp); err != nil {
				log.Printf("[Poller] 解析命令JSON失败: %v", err)
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			switch cmdResp.Command {
			case "PAUSE_ALL":
				log.Println("[Poller] 收到 PAUSE_ALL 命令")
				m.Pause()
				poller.TriggerStatsRefresh()

			case "RESUME_ALL":
				log.Println("[Poller] 收到 RESUME_ALL 命令")
				m.Resume()
				poller.TriggerStatsRefresh()
			}

			m.pollFileCommands(poller)
		}
	}
}

// pollFileCommands 轮询文件命令
func (m *Manager) pollFileCommands(poller CommandPoller) {
	m.filePauseStates.Range(func(key, value interface{}) bool {

		relativePath := key.(string)
		baseURL := strings.Replace(config.BackendCommandURL(), "/command", "/command/file", 1)
		params := url.Values{}
		params.Set("taskId", m.taskId)
		params.Set("fileRelativePath", relativePath)
		fileCommandURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

		req, err := http.NewRequest("GET", fileCommandURL, nil)
		if err != nil {
			return true
		}
		pollCtx, cancel := context.WithTimeout(m.ctx, time.Duration(config.AppConfig.HTTPTimeout)*time.Second)
		req = req.WithContext(api.ContextWithPollerMark(pollCtx))
		resp, err := api.GlobalHTTPClient.Do(req)
		cancel()

		if err != nil {
			return true
		}

		if resp.StatusCode == http.StatusOK {
			var cmdResp CommandResponse
			if err := json.NewDecoder(resp.Body).Decode(&cmdResp); err == nil {
				switch cmdResp.Command {
				case "PAUSE":
					log.Printf("[FilePoller] 收到文件 %s 的 PAUSE 命令", relativePath)
					m.PauseFile(relativePath)
					poller.TriggerStatsRefresh()

				case "RESUME":
					log.Printf("[FilePoller] 收到文件 %s 的 RESUME 命令", relativePath)
					m.ResumeFile(relativePath)
					poller.TriggerStatsRefresh()
				}
			}
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		return true
	})
}
