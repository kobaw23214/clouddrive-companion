package uploader

import (
	"bufio"
	"clouddrive-companion-withWails2/internal/config"
	"clouddrive-companion-withWails2/internal/utils"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var TaskManifestDir string

// SetupTaskManifestDir 设置任务清单目录
func SetupTaskManifestDir() {
	TaskManifestDir = filepath.Join(config.AppSupportDir, "tasks")

	if err := os.MkdirAll(TaskManifestDir, 0755); err != nil {
		log.Printf("严重错误: 无法创建任务目录 %s: %v", TaskManifestDir, err)
		os.Exit(1)
	}
	log.Printf("任务元数据目录位于: %s", TaskManifestDir)

	// 顺带初始化任务历史 SQLite。失败不致命：上传流程仍能跑，只是历史记录
	// 不会被持久化（DashboardStats 历史 tab 会暂时空）。
	if err := InitHistoryStore(); err != nil {
		log.Printf("Warning: 任务历史数据库初始化失败: %v", err)
	}
}

// WriteManifestFile 写入清单文件
func WriteManifestFile(taskId string, manifest *TaskManifest) error {
	filePath := filepath.Join(TaskManifestDir, taskId+".manifest.json")
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest file: %w", err)
	}
	log.Printf("Manifest file created: %s", filePath)
	return nil
}

// CreateEmptyCompletedLog 创建空的完成日志
func CreateEmptyCompletedLog(taskId string) error {
	filePath := filepath.Join(TaskManifestDir, taskId+".completed.log")
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create empty log file: %w", err)
	}
	f.Close()
	log.Printf("Completion log created: %s", filePath)
	return nil
}

// ReadManifestFile 读取清单文件
func ReadManifestFile(taskId string) (*TaskManifest, error) {
	filePath := filepath.Join(TaskManifestDir, taskId+".manifest.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}
	var manifest TaskManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}
	return &manifest, nil
}

// ReadCompletedLog 读取完成日志
func ReadCompletedLog(taskId string) (map[string]bool, error) {
	completedSet := make(map[string]bool)
	filePath := filepath.Join(TaskManifestDir, taskId+".completed.log")

	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return completedSet, nil
		}
		return nil, fmt.Errorf("failed to open completion log: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			completedSet[line] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan completion log: %w", err)
	}
	return completedSet, nil
}

// CalculatePendingFiles 计算待上传文件
func CalculatePendingFiles(manifest *TaskManifest, completedSet map[string]bool) []utils.FullFileInfo {
	var pendingFiles []utils.FullFileInfo
	for _, fileInfo := range manifest.AllFiles {
		if _, isCompleted := completedSet[fileInfo.RelativePath]; !isCompleted {
			pendingFiles = append(pendingFiles, fileInfo)
		}
	}
	return pendingFiles
}

// DeleteTaskFiles 删除任务文件
func DeleteTaskFiles(taskId string) error {
	manifestPath := filepath.Join(TaskManifestDir, taskId+".manifest.json")
	logPath := filepath.Join(TaskManifestDir, taskId+".completed.log")
	failedPath := filepath.Join(TaskManifestDir, taskId+".failed.log")

	err1 := os.Remove(manifestPath)
	if err1 != nil && !os.IsNotExist(err1) {
		log.Printf("Warning: failed to remove manifest file %s: %v", manifestPath, err1)
	}
	err2 := os.Remove(logPath)
	if err2 != nil && !os.IsNotExist(err2) {
		log.Printf("Warning: failed to remove log file %s: %v", logPath, err2)
	}
	err3 := os.Remove(failedPath)
	if err3 != nil && !os.IsNotExist(err3) {
		log.Printf("Warning: failed to remove failed log file %s: %v", failedPath, err3)
	}

	if err1 != nil || err2 != nil {
		return fmt.Errorf("failed to delete one or more task files")
	}

	log.Printf("Successfully cleaned up task files for %s", taskId)
	return nil
}

// logFileCompleted 记录文件完成
func (m *Manager) logFileCompleted(relativePath string) error {
	m.logFileMutex.Lock()
	defer m.logFileMutex.Unlock()

	logPath := filepath.Join(m.taskManifestDir, m.taskId+".completed.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("严重错误: 无法打开 .completed.log file for writing: %v", err)
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(relativePath + "\n"); err != nil {
		log.Printf("严重错误: 无法写入 .completed.log file: %v", err)
		return err
	}

	return f.Sync()
}

// logFileFailed 记录文件失败
func (m *Manager) logFileFailed(relativePath string) error {
	m.logFileMutex.Lock()
	defer m.logFileMutex.Unlock()

	logPath := filepath.Join(m.taskManifestDir, m.taskId+".failed.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("严重错误: 无法打开 .failed.log file for writing: %v", err)
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(relativePath + "\n"); err != nil {
		log.Printf("严重错误: 无法写入 .failed.log file: %v", err)
		return err
	}

	return f.Sync()
}

// ReadFailedLog 读取失败日志
func ReadFailedLog(taskId string) (map[string]bool, error) {
	failedSet := make(map[string]bool)
	filePath := filepath.Join(TaskManifestDir, taskId+".failed.log")

	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return failedSet, nil
		}
		return nil, fmt.Errorf("failed to open failed log: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			failedSet[line] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan failed log: %w", err)
	}
	return failedSet, nil
}

// ClearFailedLog 清空失败日志（续传时调用）
func ClearFailedLog(taskId string) {
	filePath := filepath.Join(TaskManifestDir, taskId+".failed.log")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: failed to clear failed log for task %s: %v", taskId, err)
	} else {
		log.Printf("已清空任务 %s 的失败日志", taskId)
	}
}
