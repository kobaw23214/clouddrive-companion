package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clouddrive-companion-withWails2/internal/api"

	_ "modernc.org/sqlite"
)

// HTTP 请求日志的 SQLite 持久化层
//
// 设计要点：
//   - 数据库文件: <AppSupportDir>/http_log/http_logs.db
//   - 表 http_logs: 自增 id（与中间件中的 ID 不一定一致，但更可靠）
//   - 索引 idx_logs_ts (timestamp_ms DESC) 支持倒序分页和时间范围查询
//   - 异步通道 + 单 writer goroutine + 50 条或 200 ms 批量提交，handler 永不阻塞磁盘
//   - WAL 模式 + synchronous=NORMAL，写入吞吐高且崩溃后自动恢复
//   - 暴露按需查询接口（CountHTTPLogs / QueryHTTPLogs / GetHTTPLogByID），前端按需拉取
//
// 注意：这里不再保留"内存最近 3000 条"的实时视图，
// 前端要看实时数据直接调 QueryHTTPLogs(page=1) 即可，SQLite 索引下毫秒级。

const (
	httpLogChanSize       = 4096
	httpLogBatchMaxSize   = 50
	httpLogBatchMaxLatency = 200 * time.Millisecond
)

// HTTPLogStore 异步 SQLite 写入器
type HTTPLogStore struct {
	dbPath string
	db     *sql.DB

	writeCh chan HTTPRequestLog

	statsMu sync.Mutex
	dropped uint64 // 因队列满被丢弃的条数
}

var globalHTTPLogStore *HTTPLogStore

// HTTPLogPage 请求日志分页结果
type HTTPLogPage struct {
	Total   int              `json:"total"`
	Page    int              `json:"page"`
	Size    int              `json:"size"`
	Entries []HTTPRequestLog `json:"entries"`
}

// InitHTTPLogStore 初始化全局日志存储；幂等。
// dir 一般传 <AppSupportDir>/http_log。
func InitHTTPLogStore(dir string) error {
	if globalHTTPLogStore != nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create http log dir: %w", err)
	}

	dbPath := filepath.Join(dir, "http_logs.db")
	// 启用 WAL + 适度 fsync 以提升吞吐
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)",
		filepath.ToSlash(dbPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open http log db: %w", err)
	}
	// modernc.org/sqlite 是单连接友好的，限制并发以避免 SQLITE_BUSY
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return fmt.Errorf("ping http log db: %w", err)
	}

	if err := initHTTPLogSchema(db); err != nil {
		_ = db.Close()
		return fmt.Errorf("init http log schema: %w", err)
	}

	s := &HTTPLogStore{
		dbPath:  dbPath,
		db:      db,
		writeCh: make(chan HTTPRequestLog, httpLogChanSize),
	}
	globalHTTPLogStore = s
	go s.run()
	log.Printf("HTTP 请求日志数据库已就绪: %s", dbPath)
	return nil
}

func initHTTPLogSchema(db *sql.DB) error {
	// 1. 新库直接建带全部列的表（IF NOT EXISTS 对老库是 no-op）
	createTable := `CREATE TABLE IF NOT EXISTS http_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			middleware_id INTEGER,
			timestamp TEXT NOT NULL,
			timestamp_ms INTEGER NOT NULL,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			query TEXT,
			remote_addr TEXT,
			request_headers TEXT NOT NULL,
			request_body TEXT,
			request_body_truncated INTEGER NOT NULL DEFAULT 0,
			status_code INTEGER NOT NULL,
			response_headers TEXT NOT NULL,
			response_body TEXT,
			response_body_truncated INTEGER NOT NULL DEFAULT 0,
			response_bytes INTEGER NOT NULL,
			duration_ms INTEGER NOT NULL,
			direction TEXT NOT NULL DEFAULT 'in',
			peer TEXT NOT NULL DEFAULT '',
			task_id TEXT NOT NULL DEFAULT ''
		);`
	if _, err := db.Exec(createTable); err != nil {
		return fmt.Errorf("exec %q: %w", createTable, err)
	}

	// 2. 老库平滑迁移：CREATE TABLE IF NOT EXISTS 不会给已有表补列，这里按 PRAGMA 检查补齐
	if err := migrateHTTPLogSchema(db); err != nil {
		return err
	}

	// 3. 索引（此时新列必定已存在）
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_logs_ts ON http_logs (timestamp_ms DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_path ON http_logs (path);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_status ON http_logs (status_code);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_task_id ON http_logs (task_id);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_direction ON http_logs (direction);`,
	}
	for _, stmt := range indexes {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt, err)
		}
	}
	return nil
}

// migrateHTTPLogSchema 给历史版本创建的 http_logs 表补充 direction/peer/task_id 三列。
// 通过 PRAGMA table_info 判断列是否已存在，缺失才 ALTER TABLE，天然幂等。
func migrateHTTPLogSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(http_logs)`)
	if err != nil {
		return fmt.Errorf("pragma table_info: %w", err)
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	addColumns := []struct{ name, ddl string }{
		{"direction", `ALTER TABLE http_logs ADD COLUMN direction TEXT NOT NULL DEFAULT 'in'`},
		{"peer", `ALTER TABLE http_logs ADD COLUMN peer TEXT NOT NULL DEFAULT ''`},
		{"task_id", `ALTER TABLE http_logs ADD COLUMN task_id TEXT NOT NULL DEFAULT ''`},
	}
	for _, col := range addColumns {
		if existing[col.name] {
			continue
		}
		if _, err := db.Exec(col.ddl); err != nil {
			return fmt.Errorf("exec %q: %w", col.ddl, err)
		}
	}
	return nil
}

// AppendHTTPLogToDisk 异步把日志写入磁盘；队列满时丢弃。
// 由 httpLogMiddleware 在内存追加之后再调用。
func AppendHTTPLogToDisk(entry HTTPRequestLog) {
	if globalHTTPLogStore == nil {
		return
	}
	select {
	case globalHTTPLogStore.writeCh <- entry:
	default:
		globalHTTPLogStore.statsMu.Lock()
		globalHTTPLogStore.dropped++
		dropped := globalHTTPLogStore.dropped
		globalHTTPLogStore.statsMu.Unlock()
		if dropped == 1 || dropped%200 == 0 {
			log.Printf("[httplog] 写入队列已满，已丢弃 %d 条日志", dropped)
		}
	}
}

// run 后台 writer：批量插入，最多累计 50 条或 200 ms 提交一次
func (s *HTTPLogStore) run() {
	batch := make([]HTTPRequestLog, 0, httpLogBatchMaxSize)
	timer := time.NewTimer(httpLogBatchMaxLatency)
	timer.Stop()
	timerActive := false

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.writeBatch(batch); err != nil {
			log.Printf("[httplog] 写入失败: %v", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case entry := <-s.writeCh:
			batch = append(batch, entry)
			if !timerActive {
				timer.Reset(httpLogBatchMaxLatency)
				timerActive = true
			}
			if len(batch) >= httpLogBatchMaxSize {
				if timerActive {
					timer.Stop()
					timerActive = false
				}
				flush()
			}
		case <-timer.C:
			timerActive = false
			flush()
		}
	}
}

func (s *HTTPLogStore) writeBatch(batch []HTTPRequestLog) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO http_logs (
		middleware_id, timestamp, timestamp_ms, method, path, query, remote_addr,
		request_headers, request_body, request_body_truncated,
		status_code, response_headers, response_body, response_body_truncated,
		response_bytes, duration_ms, direction, peer, task_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, e := range batch {
		reqHeaders, _ := json.Marshal(e.RequestHeaders)
		respHeaders, _ := json.Marshal(e.ResponseHeaders)

		_, err := stmt.ExecContext(ctx,
			e.ID,
			e.Timestamp,
			e.TimestampMs,
			e.Method,
			e.Path,
			nullableString(e.Query),
			e.RemoteAddr,
			string(reqHeaders),
			nullableString(e.RequestBody),
			boolToInt(e.RequestBodyTrunc),
			e.StatusCode,
			string(respHeaders),
			nullableString(e.ResponseBody),
			boolToInt(e.ResponseBodyTrunc),
			e.ResponseBytes,
			e.DurationMs,
			e.Direction,
			e.Peer,
			e.TaskID,
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// HTTPLogQuery 查询参数
type HTTPLogQuery struct {
	Page         int    `json:"page"`         // 1-based
	Size         int    `json:"size"`
	Method       string `json:"method"`       // 可选：过滤 HTTP 方法
	PathLike     string `json:"pathLike"`     // 可选：路径模糊匹配（LIKE）
	StatusGroup  string `json:"statusGroup"`  // 可选: "2xx" | "3xx" | "4xx" | "5xx"
	StartMs      int64  `json:"startMs"`      // 可选：>=
	EndMs        int64  `json:"endMs"`        // 可选：<
	ExcludePing  bool   `json:"excludePing"`  // 可选：屏蔽 /ping 请求
}

// httpLogSelectCols 查询单条日志时的完整列清单（与 scanLogRow 的 Scan 顺序一一对应）
const httpLogSelectCols = `id, middleware_id, timestamp, timestamp_ms, method, path, query, remote_addr,
	        request_headers, request_body, request_body_truncated,
	        status_code, response_headers, response_body, response_body_truncated,
	        response_bytes, duration_ms, direction, peer, task_id`

// CountHTTPLogs 返回符合过滤条件的总条数
func CountHTTPLogs(q HTTPLogQuery) (int, error) {
	if globalHTTPLogStore == nil {
		return 0, fmt.Errorf("http log store not initialized")
	}
	where, args := buildWhere(q)
	row := globalHTTPLogStore.db.QueryRow(`SELECT COUNT(*) FROM http_logs `+where, args...)
	var total int
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// QueryHTTPLogs 按时间倒序分页查询符合条件的日志
func QueryHTTPLogs(q HTTPLogQuery) (HTTPLogPage, error) {
	empty := HTTPLogPage{Page: q.Page, Size: q.Size, Entries: []HTTPRequestLog{}}
	if globalHTTPLogStore == nil {
		return empty, fmt.Errorf("http log store not initialized")
	}

	if q.Page < 1 {
		q.Page = 1
	}
	if q.Size < 1 {
		q.Size = 50
	}

	total, err := CountHTTPLogs(q)
	if err != nil {
		return empty, err
	}

	where, args := buildWhere(q)
	offset := (q.Page - 1) * q.Size
	args = append(args, q.Size, offset)
	rows, err := globalHTTPLogStore.db.Query(
		`SELECT `+httpLogSelectCols+`
		   FROM http_logs `+where+`
		  ORDER BY timestamp_ms DESC, id DESC
		  LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return empty, err
	}
	defer rows.Close()

	entries := make([]HTTPRequestLog, 0, q.Size)
	for rows.Next() {
		entry, err := scanLogRow(rows)
		if err != nil {
			return empty, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return empty, err
	}

	return HTTPLogPage{
		Total:   total,
		Page:    q.Page,
		Size:    q.Size,
		Entries: entries,
	}, nil
}

// GetHTTPLogByID 按内部主键拉取一条完整日志（详情视图用）
func GetHTTPLogByID(id int64) (*HTTPRequestLog, error) {
	if globalHTTPLogStore == nil {
		return nil, fmt.Errorf("http log store not initialized")
	}
	row := globalHTTPLogStore.db.QueryRow(
		`SELECT `+httpLogSelectCols+`
		   FROM http_logs WHERE id = ?`, id)
	entry, err := scanLogRow(row)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// QuerySequenceEvents 时序图数据查询：按 timestamp_ms 正序返回指定时间范围内的日志。
//
//	fromMs/toMs: 毫秒时间戳，0 表示不限制（fromMs 为 >=，toMs 为 <）
//	taskID:      可选，非空时只返回该任务的日志
//	direction:   可选，"in"/"out"，空串表示全部
//	page/size:   1-based 分页；返回条目与符合条件的总数 total
func QuerySequenceEvents(fromMs, toMs int64, taskID string, direction string, page, size int) ([]HTTPRequestLog, int, error) {
	empty := []HTTPRequestLog{}
	if globalHTTPLogStore == nil {
		return empty, 0, fmt.Errorf("http log store not initialized")
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 200
	}

	var (
		clauses []string
		args    []interface{}
	)
	if fromMs > 0 {
		clauses = append(clauses, "timestamp_ms >= ?")
		args = append(args, fromMs)
	}
	if toMs > 0 {
		clauses = append(clauses, "timestamp_ms < ?")
		args = append(args, toMs)
	}
	if taskID != "" {
		clauses = append(clauses, "task_id = ?")
		args = append(args, taskID)
	}
	if direction != "" {
		clauses = append(clauses, "direction = ?")
		args = append(args, direction)
	}
	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}

	var total int
	row := globalHTTPLogStore.db.QueryRow(`SELECT COUNT(*) FROM http_logs `+where, args...)
	if err := row.Scan(&total); err != nil {
		return empty, 0, err
	}

	offset := (page - 1) * size
	queryArgs := append(append([]interface{}{}, args...), size, offset)
	rows, err := globalHTTPLogStore.db.Query(
		`SELECT `+httpLogSelectCols+`
		   FROM http_logs `+where+`
		  ORDER BY timestamp_ms ASC, id ASC
		  LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return empty, 0, err
	}
	defer rows.Close()

	entries := make([]HTTPRequestLog, 0, size)
	for rows.Next() {
		entry, err := scanLogRow(rows)
		if err != nil {
			return empty, 0, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return empty, 0, err
	}
	return entries, total, nil
}

// LogOutboundHTTP 出站请求日志入口：由 api 包通过 SetOutboundLogger 回调进来，
// 转成统一的 HTTPRequestLog（direction="out"）走现有的异步批量写入通道。
// 放在 server 包是为了复用 AppendHTTPLogToDisk；依赖方向 server → api 不产生 import 环。
func LogOutboundHTTP(e api.OutboundLogEntry) {
	// 网络错误没有响应体，把错误串放进 responseBody 方便前端排查
	respBody := e.ResponseBody
	if e.Error != "" && respBody == "" {
		respBody = e.Error
	}
	AppendHTTPLogToDisk(HTTPRequestLog{
		Timestamp:         time.UnixMilli(e.TimestampMs).Format("2006-01-02 15:04:05.000"),
		TimestampMs:       e.TimestampMs,
		Method:            e.Method,
		Path:              e.Path,
		Query:             e.Query,
		RemoteAddr:        e.Peer,
		Peer:              e.Peer,
		Direction:         "out",
		TaskID:            e.TaskID,
		RequestHeaders:    e.RequestHeaders,
		RequestBody:       e.RequestBody,
		RequestBodyTrunc:  e.RequestBodyTrunc,
		StatusCode:        e.StatusCode,
		ResponseHeaders:   e.ResponseHeaders,
		ResponseBody:      respBody,
		ResponseBodyTrunc: e.ResponseBodyTrunc,
		ResponseBytes:     int64(len(e.ResponseBody)),
		DurationMs:        e.DurationMs,
	})
}

// HTTPLogStats 数据库整体统计，给前端展示总条数和数据库占用
type HTTPLogStats struct {
	Total      int    `json:"total"`
	DBPath     string `json:"dbPath"`
	DBSize     int64  `json:"dbSize"`
	DroppedMem uint64 `json:"droppedMem"`
}

// GetHTTPLogStats 获取数据库整体统计
func GetHTTPLogStats() HTTPLogStats {
	if globalHTTPLogStore == nil {
		return HTTPLogStats{}
	}
	total, _ := CountHTTPLogs(HTTPLogQuery{})

	var dbSize int64
	if fi, err := os.Stat(globalHTTPLogStore.dbPath); err == nil {
		dbSize = fi.Size()
		// WAL 文件也算上
		if w, err := os.Stat(globalHTTPLogStore.dbPath + "-wal"); err == nil {
			dbSize += w.Size()
		}
	}

	globalHTTPLogStore.statsMu.Lock()
	dropped := globalHTTPLogStore.dropped
	globalHTTPLogStore.statsMu.Unlock()

	return HTTPLogStats{
		Total:      total,
		DBPath:     globalHTTPLogStore.dbPath,
		DBSize:     dbSize,
		DroppedMem: dropped,
	}
}

// ----- 内部工具 -----

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanLogRow(r rowScanner) (HTTPRequestLog, error) {
	var (
		entry          HTTPRequestLog
		middlewareID   sql.NullInt64
		query          sql.NullString
		reqBody        sql.NullString
		reqHeaders     string
		respHeaders    string
		respBody       sql.NullString
		reqTrunc       int
		respTrunc      int
		dbID           int64
	)
	err := r.Scan(
		&dbID,
		&middlewareID,
		&entry.Timestamp,
		&entry.TimestampMs,
		&entry.Method,
		&entry.Path,
		&query,
		&entry.RemoteAddr,
		&reqHeaders,
		&reqBody,
		&reqTrunc,
		&entry.StatusCode,
		&respHeaders,
		&respBody,
		&respTrunc,
		&entry.ResponseBytes,
		&entry.DurationMs,
		&entry.Direction,
		&entry.Peer,
		&entry.TaskID,
	)
	if err != nil {
		return entry, err
	}
	// 把数据库主键作为给前端用的稳定 ID
	entry.ID = uint64(dbID)
	if query.Valid {
		entry.Query = query.String
	}
	if reqBody.Valid {
		entry.RequestBody = reqBody.String
	}
	if respBody.Valid {
		entry.ResponseBody = respBody.String
	}
	entry.RequestBodyTrunc = reqTrunc != 0
	entry.ResponseBodyTrunc = respTrunc != 0
	if reqHeaders != "" {
		_ = json.Unmarshal([]byte(reqHeaders), &entry.RequestHeaders)
	}
	if respHeaders != "" {
		_ = json.Unmarshal([]byte(respHeaders), &entry.ResponseHeaders)
	}
	return entry, nil
}

func buildWhere(q HTTPLogQuery) (string, []interface{}) {
	var (
		clauses []string
		args    []interface{}
	)
	if q.Method != "" {
		clauses = append(clauses, "method = ?")
		args = append(args, q.Method)
	}
	if q.PathLike != "" {
		clauses = append(clauses, "path LIKE ?")
		args = append(args, "%"+q.PathLike+"%")
	}
	switch q.StatusGroup {
	case "2xx":
		clauses = append(clauses, "status_code >= 200 AND status_code < 300")
	case "3xx":
		clauses = append(clauses, "status_code >= 300 AND status_code < 400")
	case "4xx":
		clauses = append(clauses, "status_code >= 400 AND status_code < 500")
	case "5xx":
		clauses = append(clauses, "status_code >= 500 AND status_code < 600")
	}
	if q.StartMs > 0 {
		clauses = append(clauses, "timestamp_ms >= ?")
		args = append(args, q.StartMs)
	}
	if q.EndMs > 0 {
		clauses = append(clauses, "timestamp_ms < ?")
		args = append(args, q.EndMs)
	}
	if q.ExcludePing {
		clauses = append(clauses, "path != ?")
		args = append(args, "/ping")
	}
	if len(clauses) == 0 {
		return "", args
	}
	where := "WHERE "
	for i, c := range clauses {
		if i > 0 {
			where += " AND "
		}
		where += c
	}
	return where, args
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DeleteHTTPLogs 根据 ID 列表批量删除 HTTP 日志。如果 ids 为空，则删除所有。
func DeleteHTTPLogs(ids []uint64) error {
	if globalHTTPLogStore == nil {
		return fmt.Errorf("http log store not initialized")
	}

	if len(ids) == 0 {
		_, err := globalHTTPLogStore.db.Exec(`DELETE FROM http_logs`)
		if err == nil {
			globalHTTPLogStore.db.Exec(`VACUUM`)
		}
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := globalHTTPLogStore.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `DELETE FROM http_logs WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
