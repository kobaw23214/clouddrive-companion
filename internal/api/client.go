package api

import (
	"clouddrive-companion-withWails2/internal/config"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var GlobalHTTPClient *http.Client

// InitClient 初始化全局 HTTP 客户端（包一层出站日志 RoundTripper；
// 未注册 logger 或配置关闭时为零开销透传）
func InitClient() {
	client := config.InitHTTPClient()
	client.Transport = &loggingTransport{base: client.Transport}
	GlobalHTTPClient = client
}

// SendTaskCommand 向后端Java API发送任务级的命令（暂停/恢复）
func SendTaskCommand(taskId string, action string) error {
	commandURL := config.BackendCommandURL()
	baseURL := strings.Replace(commandURL, "/command", "/"+action, 1)
	if baseURL == commandURL {
		return fmt.Errorf("无法从 BackendCommandURL 推断出 /%s 路径", action)
	}

	fullURL := fmt.Sprintf("%s/%s", baseURL, taskId)
	log.Printf("[API Call]发送任务命令: POST %s", fullURL)

	req, err := http.NewRequest("POST", fullURL, nil)
	if err != nil {
		return fmt.Errorf("创建任务命令请求失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.AppConfig.HTTPTimeout)*time.Second)
	defer cancel()
	req = req.WithContext(ContextWithTaskID(ctx, taskId))

	resp, err := GlobalHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("发送任务命令失败: %w", err)
	}

	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("服务器返回错误: %s", resp.Status)
	}

	log.Printf("[API Call]任务 %s 的 %s 命令发送成功", taskId, action)
	return nil
}

// SendFileCommand 向后端Java API发送文件级的命令（暂停/恢复）
func SendFileCommand(taskId string, relativePath string, action string) error {
	commandURL := config.BackendCommandURL()
	baseURL := strings.Replace(commandURL, "/command", "/"+action+"/file", 1)
	if baseURL == commandURL {
		return fmt.Errorf("无法从 BackendCommandURL 推断出 /%s/file 路径", action)
	}

	params := url.Values{}
	params.Set("taskId", taskId)
	params.Set("fileRelativePath", relativePath)
	fullURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	log.Printf("[API Call]发送文件命令: POST %s", fullURL)

	req, err := http.NewRequest("POST", fullURL, nil)
	if err != nil {
		return fmt.Errorf("创建文件命令请求失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.AppConfig.HTTPTimeout)*time.Second)
	defer cancel()
	req = req.WithContext(ContextWithTaskID(ctx, taskId))

	resp, err := GlobalHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("发送文件命令失败: %w", err)
	}

	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("服务器返回错误: %s", resp.Status)
	}

	log.Printf("[API Call]文件 %s (任务 %s) 的 %s 命令发送成功", relativePath, taskId, action)
	return nil
}

// NotifyBackendTaskComplete 通知后端任务已完成
func NotifyBackendTaskComplete(taskId string) error {
	data := url.Values{}
	data.Set("taskId", taskId)

	req, err := http.NewRequest("POST", config.BackendCompleteURL(), strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("创建完成通知请求失败: %w", err)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.AppConfig.HTTPTimeout)*time.Second)
	defer cancel()
	req = req.WithContext(ContextWithTaskID(ctx, taskId))

	resp, err := GlobalHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("发送完成通知失败: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("服务器返回非200状态: %s - %s", resp.Status, string(body))
	}

	log.Printf("任务 %s 完成通知已发送", taskId)
	return nil
}
