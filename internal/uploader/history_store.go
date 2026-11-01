package uploader

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// 任务历史持久化层。
//
// 设计目标：让"已完成"任务的记录在 manifest/.completed.log 等本地工件被清理后
// 仍能保留，避免历史记录依赖易失文件。每次任务跑完一次性写入完整快照（任务
// 元信息 + 全部文件 FileProgress 列表 JSON），后续查询都直接读 SQLite。
//
// 这里没有走 tracer 那种异步 batch writer：任务完成是低频事件（人手级别），
// 同步 INSERT 更直观也更安全（写完才返回，调用方接着删 manifest 不会有数据
// 丢失窗口）。

// HistoryRecord 一次任务完成时的完整快照。
type HistoryRecord struct {
	TaskId         string
	TaskName       string
	UserId         string
	UserName       string
	Dir            string
	Status         string // "COMPLETED" / "PARTIAL_COMPLETED" / "INTERRUPTED"
	TotalSize      int64
	UploadedSize   int64
	TotalFiles     int
	CompletedFiles int
	FailedFiles    int
	Files          []FileProgress
	TraceId        string
	CreatedAtMs    int64
	FinishedAtMs   int64
}

var (
	historyDB     *sql.DB
	historyDBPath string
	historyMu     sync.Mutex
)

// InitHistoryStore 在 TaskManifestDir 旁打开 task_history.db。幂等。
func InitHistoryStore() error {
	historyMu.Lock()
	defer historyMu.Unlock()
	if historyDB != nil {
		return nil
	}
	if TaskManifestDir == "" {
		return fmt.Errorf("TaskManifestDir 未初始化，请先调用 SetupTaskManifestDir")
	}

	historyDBPath = filepath.Join(TaskManifestDir, "task_history.db")
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)",
		filepath.ToSlash(historyDBPath),
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open history db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return fmt.Errorf("ping history db: %w", err)
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS task_history (
			task_id          TEXT PRIMARY KEY,
			task_name        TEXT NOT NULL DEFAULT '',
			user_id          TEXT NOT NULL DEFAULT '',
			user_name        TEXT NOT NULL DEFAULT '',
			dir              TEXT NOT NULL DEFAULT '',
			status           TEXT NOT NULL,
			total_size       INTEGER NOT NULL DEFAULT 0,
			uploaded_size    INTEGER NOT NULL DEFAULT 0,
			total_files      INTEGER NOT NULL DEFAULT 0,
			completed_files  INTEGER NOT NULL DEFAULT 0,
			failed_files     INTEGER NOT NULL DEFAULT 0,
			files_json       TEXT NOT NULL DEFAULT '[]',
			trace_id         TEXT NOT NULL DEFAULT '',
			created_at_ms    INTEGER NOT NULL,
			finished_at_ms   INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS ix_history_finished ON task_history(finished_at_ms DESC);`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			_ = db.Close()
			return fmt.Errorf("init history schema: %w", err)
		}
	}

	historyDB = db
	log.Printf("任务历史数据库已就绪: %s", historyDBPath)
	return nil
}

// ShutdownHistoryStore 关闭数据库。
func ShutdownHistoryStore() {
	historyMu.Lock()
	defer historyMu.Unlock()
	if historyDB != nil {
		_ = historyDB.Close()
		historyDB = nil
	}
}

// SaveHistoryRecord 同步写入一条历史记录。同 task_id 时覆盖（用 INSERT OR REPLACE）。
//
// 调用时机：StartUploadProcess 在删 manifest 前调用一次。即使写库失败也不阻塞
// 删除流程（仅打 warn log），避免历史 db 故障让上传流程卡住。
func SaveHistoryRecord(rec HistoryRecord) error {
	historyMu.Lock()
	db := historyDB
	historyMu.Unlock()
	if db == nil {
		return fmt.Errorf("history db not initialized")
	}

	filesJSON, err := json.Marshal(rec.Files)
	if err != nil {
		return fmt.Errorf("marshal files: %w", err)
	}

	now := time.Now().UnixMilli()
	if rec.FinishedAtMs == 0 {
		rec.FinishedAtMs = now
	}
	if rec.CreatedAtMs == 0 {
		rec.CreatedAtMs = now
	}

	_, err = db.Exec(`
		INSERT OR REPLACE INTO task_history
		(task_id, task_name, user_id, user_name, dir, status,
		 total_size, uploaded_size, total_files, completed_files, failed_files,
		 files_json, trace_id, created_at_ms, finished_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rec.TaskId, rec.TaskName, rec.UserId, rec.UserName, rec.Dir, rec.Status,
		rec.TotalSize, rec.UploadedSize, rec.TotalFiles, rec.CompletedFiles, rec.FailedFiles,
		string(filesJSON), rec.TraceId, rec.CreatedAtMs, rec.FinishedAtMs,
	)
	if err != nil {
		return fmt.Errorf("insert history: %w", err)
	}
	return nil
}

// ListHistoryRecords 按 finished_at_ms 倒序返回所有历史。不返回 files_json
// 字段（前端按需通过 GetHistoryFiles 拉取，避免一次性加载大列表）。
func ListHistoryRecords() ([]HistoryRecord, error) {
	historyMu.Lock()
	db := historyDB
	historyMu.Unlock()
	if db == nil {
		return nil, fmt.Errorf("history db not initialized")
	}
	rows, err := db.Query(`
		SELECT task_id, task_name, user_id, user_name, dir, status,
		       total_size, uploaded_size, total_files, completed_files, failed_files,
		       trace_id, created_at_ms, finished_at_ms
		FROM task_history
		ORDER BY finished_at_ms DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	var out []HistoryRecord
	for rows.Next() {
		var r HistoryRecord
		if err := rows.Scan(
			&r.TaskId, &r.TaskName, &r.UserId, &r.UserName, &r.Dir, &r.Status,
			&r.TotalSize, &r.UploadedSize, &r.TotalFiles, &r.CompletedFiles, &r.FailedFiles,
			&r.TraceId, &r.CreatedAtMs, &r.FinishedAtMs,
		); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

// GetHistoryFiles 返回某历史任务的全量文件列表（已反序列化 files_json）。
//
// 排序与分页交给上层（与 manifest 路径的 SortFiles / 切片逻辑保持一致）。
func GetHistoryFiles(taskId string) ([]FileProgress, error) {
	historyMu.Lock()
	db := historyDB
	historyMu.Unlock()
	if db == nil {
		return nil, fmt.Errorf("history db not initialized")
	}
	var filesJSON string
	err := db.QueryRow(`SELECT files_json FROM task_history WHERE task_id = ?`, taskId).Scan(&filesJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query files: %w", err)
	}
	var files []FileProgress
	if err := json.Unmarshal([]byte(filesJSON), &files); err != nil {
		return nil, fmt.Errorf("unmarshal files: %w", err)
	}
	return files, nil
}

// DeleteHistoryRecord 按 task_id 删除一条记录。
func DeleteHistoryRecord(taskId string) error {
	historyMu.Lock()
	db := historyDB
	historyMu.Unlock()
	if db == nil {
		return fmt.Errorf("history db not initialized")
	}
	_, err := db.Exec(`DELETE FROM task_history WHERE task_id = ?`, taskId)
	if err != nil {
		return fmt.Errorf("delete history: %w", err)
	}
	return nil
}

// HasHistoryRecord 用于让 ListHistoricalTasks 判断某个 manifest 是否已经
// 在 sqlite 里有记录（避免重复显示）。
func HasHistoryRecord(taskId string) bool {
	historyMu.Lock()
	db := historyDB
	historyMu.Unlock()
	if db == nil {
		return false
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_history WHERE task_id = ?`, taskId).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// historyDBSize 返回 db 文件大小，给 stats / 测试用。
func historyDBSize() int64 {
	if historyDBPath == "" {
		return 0
	}
	info, err := os.Stat(historyDBPath)
	if err != nil {
		return 0
	}
	return info.Size()
}
