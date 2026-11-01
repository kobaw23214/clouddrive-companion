package tracer

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// task_traces 持久化层 —— 与 internal/server/httplog_store.go 同构：
//
//	modernc.org/sqlite 驱动 + WAL + 单连接 + 异步攒批 flush。
//
// 设计要点：
//   - writeCh 容量 4096，溢出时丢弃 + warn 日志而非阻塞调用方
//   - 攒批 100 条或 500 ms 提交一次
//   - 单事务 prepared statement 批量插入
//   - Shutdown 通过 closeCh 触发：排空 channel 中的剩余条目，最多 3 秒
//
// 注意：modernc/sqlite 单连接友好（避免 SQLITE_BUSY），所以 MaxOpenConns=1。

const (
	storeBatchMaxSize    = 100
	storeBatchMaxLatency = 500 * time.Millisecond
	storeChannelSize     = 4096
)

// spanWriteOp 表示一次写操作的种类与载荷。
//
// 'i' = INSERT（首次落库，对应 startSpan / RecordSpan）
// 'u' = UPDATE（回填结束信息，对应 finish）
//
// 设计原因：方案 A 要求 span 在 Start 时就以 "end_ms=0" 形式入库，让前端能在
// 任务进行中也看到链路。finish 时再回填结束字段。两种写动作走同一个有序队列，
// FIFO 顺序保证同一 span_id 的 UPDATE 一定在 INSERT 之后被执行。
type spanWriteOp struct {
	op   byte
	span *Span
}

// SpanBroadcaster 在 writeBatch 成功后被 Store 调用，把刚落库的写操作广播给
// SSE 订阅者。tracer 包用 traceHub 实现该接口；测试时可注入 nil 跳过广播。
type SpanBroadcaster interface {
	BroadcastInsert(s *Span)
	BroadcastUpdate(s *Span)
}

// Store 异步 SQLite 写入器与查询入口。
type Store struct {
	dbPath  string
	db      *sql.DB
	writeCh chan spanWriteOp

	closeOnce sync.Once
	closeCh   chan struct{} // 通知 run() 准备退出
	doneCh    chan struct{} // run() 已经退出

	dropped atomic.Uint64 // 因队列满被丢弃的 op 数

	// broadcaster 在每个 batch 落库成功后被调用一次，让 SSE Hub 把变更
	// 通知给已订阅的前端。允许为 nil（测试 / 早期初始化阶段）。
	broadcaster SpanBroadcaster
}

// openStore 打开 <dir>/traces.db，建表并启动后台 writer。
func openStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("tracer: openStore 需要非空 dir")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("tracer: 创建追踪目录失败: %w", err)
	}
	dbPath := filepath.Join(dir, "traces.db")

	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)",
		filepath.ToSlash(dbPath),
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("tracer: 打开数据库失败: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("tracer: ping 数据库失败: %w", err)
	}
	if err := initTraceSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("tracer: 初始化 schema 失败: %w", err)
	}

	s := &Store{
		dbPath:  dbPath,
		db:      db,
		writeCh: make(chan spanWriteOp, storeChannelSize),
		closeCh: make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	go s.run()
	log.Printf("[tracer] 数据库已就绪: %s", dbPath)
	return s, nil
}

// SetBroadcaster 注入 SSE Hub。在 openStore 之后、Init 后期被调用。
// 允许 nil，nil 时所有 broadcast 调用是 no-op。
func (s *Store) SetBroadcaster(b SpanBroadcaster) {
	if s == nil {
		return
	}
	s.broadcaster = b
}

// initTraceSchema 建表与索引。幂等。
func initTraceSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS task_traces (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			span_id         TEXT NOT NULL,
			parent_span_id  TEXT NOT NULL DEFAULT '',
			trace_id        TEXT NOT NULL,
			task_id         TEXT NOT NULL DEFAULT '',
			run_seq         INTEGER NOT NULL DEFAULT 1,
			trace_type      TEXT NOT NULL DEFAULT '',
			stage           TEXT NOT NULL,
			filename        TEXT NOT NULL DEFAULT '',
			start_ms        INTEGER NOT NULL,
			end_ms          INTEGER NOT NULL DEFAULT 0,
			duration_ms     INTEGER NOT NULL DEFAULT 0,
			success         INTEGER NOT NULL DEFAULT 1,
			error_message   TEXT,
			extra           TEXT,
			created_at_ms   INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS ix_traces_trace_id        ON task_traces(trace_id);`,
		`CREATE INDEX IF NOT EXISTS ix_traces_task_id         ON task_traces(task_id);`,
		`CREATE INDEX IF NOT EXISTS ix_traces_parent_span_id  ON task_traces(parent_span_id);`,
		`CREATE INDEX IF NOT EXISTS ix_traces_span_id         ON task_traces(span_id);`,
		`CREATE INDEX IF NOT EXISTS ix_traces_type_start      ON task_traces(trace_type, start_ms DESC);`,
		`CREATE INDEX IF NOT EXISTS ix_traces_created_at      ON task_traces(created_at_ms DESC);`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("exec %q: %w", q, err)
		}
	}
	return nil
}

// Enqueue 把一条新的 span 投递到写入队列（INSERT）。
// 非阻塞：队列满时丢弃 + warn 日志。
func (s *Store) Enqueue(span *Span) {
	s.enqueueOp(spanWriteOp{op: 'i', span: span})
}

// EnqueueUpdate 投递一条 UPDATE：仅回填 end_ms / duration_ms / success /
// error_message / extra 五个字段（用 span_id 作 WHERE 条件）。其余字段不变。
//
// 非阻塞：队列满时丢弃 + warn 日志。
func (s *Store) EnqueueUpdate(span *Span) {
	s.enqueueOp(spanWriteOp{op: 'u', span: span})
}

func (s *Store) enqueueOp(op spanWriteOp) {
	if s == nil || op.span == nil {
		return
	}
	select {
	case s.writeCh <- op:
	default:
		dropped := s.dropped.Add(1)
		// 仅每 100 条日志一次，避免刷屏
		if dropped%100 == 1 {
			log.Printf("[tracer] 写队列已满，累计丢弃 %d 条 (op=%c trace=%s stage=%s)",
				dropped, op.op, op.span.TraceID, op.span.Stage)
		}
	}
}

// Dropped 返回被丢弃的 span 数量（运维查询用）。
func (s *Store) Dropped() uint64 {
	if s == nil {
		return 0
	}
	return s.dropped.Load()
}

// NextRunSeq 查询并返回某 taskId 的下一个 runSeq。
//
// 没有历史记录时返回 1，否则 max(run_seq)+1。
func (s *Store) NextRunSeq(taskId string) int {
	if s == nil || taskId == "" {
		return 1
	}
	var maxSeq sql.NullInt64
	err := s.db.QueryRow(
		`SELECT MAX(run_seq) FROM task_traces WHERE task_id = ?`, taskId,
	).Scan(&maxSeq)
	if err != nil || !maxSeq.Valid {
		return 1
	}
	return int(maxSeq.Int64) + 1
}

// run 后台 writer 循环：攒批 storeBatchMaxSize 条或 storeBatchMaxLatency 提交一次。
func (s *Store) run() {
	defer close(s.doneCh)

	batch := make([]spanWriteOp, 0, storeBatchMaxSize)
	timer := time.NewTimer(storeBatchMaxLatency)
	timer.Stop()
	timerActive := false

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.writeBatch(batch); err != nil {
			log.Printf("[tracer] 批量写入失败 (丢失 %d 条): %v", len(batch), err)
		}
		batch = batch[:0]
	}

	drainAndExit := func() {
		// 收到关闭信号：排空 writeCh 后做最后一次 flush
		for {
			select {
			case op := <-s.writeCh:
				batch = append(batch, op)
			default:
				flush()
				return
			}
		}
	}

	for {
		select {
		case op, ok := <-s.writeCh:
			if !ok {
				flush()
				return
			}
			batch = append(batch, op)
			if !timerActive {
				timer.Reset(storeBatchMaxLatency)
				timerActive = true
			}
			if len(batch) >= storeBatchMaxSize {
				if timerActive {
					timer.Stop()
					timerActive = false
				}
				flush()
			}
		case <-timer.C:
			timerActive = false
			flush()
		case <-s.closeCh:
			if timerActive {
				timer.Stop()
				timerActive = false
			}
			drainAndExit()
			return
		}
	}
}

// writeBatch 用单事务批量执行 INSERT 与 UPDATE。
//
// 同 batch 中保留 FIFO：一个 span 的 INSERT 与后续 UPDATE 都按入队顺序写入。
// 简单起见用两条 prepared statement 共享同一事务，按 op 类型分发；
// 这是个清晰且足够快的实现（每条最多两次 stmt 切换）。
func (s *Store) writeBatch(batch []spanWriteOp) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	insStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO task_traces
			(span_id, parent_span_id, trace_id, task_id, run_seq,
			 trace_type, stage, filename, start_ms, end_ms, duration_ms,
			 success, error_message, extra, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer insStmt.Close()
	updStmt, err := tx.PrepareContext(ctx, `
		UPDATE task_traces
		SET end_ms = ?, duration_ms = ?, success = ?, error_message = ?, extra = ?
		WHERE span_id = ?
	`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare update: %w", err)
	}
	defer updStmt.Close()

	for _, item := range batch {
		sp := item.span
		successInt := 0
		if sp.Success {
			successInt = 1
		}
		switch item.op {
		case 'i':
			if _, err := insStmt.ExecContext(ctx,
				sp.SpanID, sp.ParentSpanID, sp.TraceID, sp.TaskID, sp.RunSeq,
				sp.TraceType, sp.Stage, sp.Filename, sp.StartMs, sp.EndMs, sp.DurationMs,
				successInt, nullableString(sp.ErrorMessage), nullableString(sp.Extra), sp.CreatedAtMs,
			); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("exec insert: %w", err)
			}
		case 'u':
			if _, err := updStmt.ExecContext(ctx,
				sp.EndMs, sp.DurationMs, successInt,
				nullableString(sp.ErrorMessage), nullableString(sp.Extra),
				sp.SpanID,
			); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("exec update: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// 事务提交成功后再广播。注意：失败回滚后不广播，避免前端看到"虚假"的事件。
	if s.broadcaster != nil {
		for _, item := range batch {
			switch item.op {
			case 'i':
				s.broadcaster.BroadcastInsert(item.span)
			case 'u':
				s.broadcaster.BroadcastUpdate(item.span)
			}
		}
	}
	return nil
}

// writeInsertBatch 是测试工具：把 []*Span 直接当作 INSERT 批次写入。
//
// 仅用于测试中绕过 channel 的同步写入路径。生产代码请走 Enqueue/EnqueueUpdate。
func (s *Store) writeInsertBatch(batch []*Span) error {
	ops := make([]spanWriteOp, len(batch))
	for i, sp := range batch {
		ops[i] = spanWriteOp{op: 'i', span: sp}
	}
	return s.writeBatch(ops)
}
func (s *Store) Shutdown(timeout time.Duration) {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		close(s.closeCh)
	})
	select {
	case <-s.doneCh:
	case <-time.After(timeout):
		log.Printf("[tracer] Shutdown 超时 %s，强制结束", timeout)
	}
	_ = s.db.Close()
}

// nullableString 让空字符串以 NULL 形式入库，节省空间。
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// dbFileSize 返回数据库文件字节数。读不到时返回 0。
func dbFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}


// ─── 清理与维护 ───

// startCleanupLoop 启动后台清理协程：启动后立即跑一次，之后每小时一次。
//
// 单独作为方法暴露而不是内置在 openStore 中，便于测试时禁用或手动驱动。
func (s *Store) startCleanupLoop(getMaxAgeDays, getMaxRows, getVacuumThresholdMB func() int) {
	if s == nil {
		return
	}
	go func() {
		s.runCleanupOnce(getMaxAgeDays(), getMaxRows(), getVacuumThresholdMB())
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.runCleanupOnce(getMaxAgeDays(), getMaxRows(), getVacuumThresholdMB())
			case <-s.closeCh:
				return
			}
		}
	}()
}

// runCleanupOnce 执行一次清理：按时间删 → 按行数删 → 必要时 VACUUM。
func (s *Store) runCleanupOnce(maxAgeDays, maxRows, vacuumThresholdMB int) {
	deleted, err := s.Cleanup(maxAgeDays, maxRows)
	if err != nil {
		log.Printf("[tracer] cleanup 失败: %v", err)
		return
	}
	if deleted > 0 {
		log.Printf("[tracer] cleanup 删除 %d 条过期 span", deleted)
	}
	if vacuumThresholdMB > 0 {
		size := dbFileSize(s.dbPath)
		if size > int64(vacuumThresholdMB)*1024*1024 {
			log.Printf("[tracer] 数据库 %d MB 超阈值 %d MB，执行 VACUUM",
				size/1024/1024, vacuumThresholdMB)
			if _, err := s.db.Exec("VACUUM"); err != nil {
				log.Printf("[tracer] VACUUM 失败: %v", err)
			}
		}
	}
}

// Cleanup 同步执行一次清理，返回删除的总行数。
//
//	maxAgeDays > 0：删除 created_at_ms < now - maxAgeDays * 1d 的 span
//	maxRows    > 0：若总行数超出，按 created_at_ms 升序删除最旧的 (n - maxRows) 行
//
// 两条规则独立执行；都为 0 时直接返回 0。
func (s *Store) Cleanup(maxAgeDays, maxRows int) (int, error) {
	if s == nil {
		return 0, nil
	}
	var totalDeleted int

	if maxAgeDays > 0 {
		cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour).UnixMilli()
		res, err := s.db.Exec(`DELETE FROM task_traces WHERE created_at_ms < ?`, cutoff)
		if err != nil {
			return totalDeleted, fmt.Errorf("delete by age: %w", err)
		}
		n, _ := res.RowsAffected()
		totalDeleted += int(n)
	}

	if maxRows > 0 {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM task_traces`).Scan(&count); err != nil {
			return totalDeleted, fmt.Errorf("count rows: %w", err)
		}
		if count > maxRows {
			toDelete := count - maxRows
			res, err := s.db.Exec(`
				DELETE FROM task_traces
				WHERE id IN (
					SELECT id FROM task_traces
					ORDER BY created_at_ms ASC, id ASC
					LIMIT ?
				)`, toDelete)
			if err != nil {
				return totalDeleted, fmt.Errorf("delete by row count: %w", err)
			}
			n, _ := res.RowsAffected()
			totalDeleted += int(n)
		}
	}
	return totalDeleted, nil
}

// SweepOrphans 启动时调用一次，把"end_ms=0 且超过 maxStaleMinutes 分钟"的 span
// 标记为失败孤儿（进程异常退出残留）。
//
// 阈值含义：单文件上传基本不会超过 60 分钟（GB 级 ISO 上传亦然），所以
// 默认 60 分钟既能避免误伤当前正在跑的长任务，又能让用户进入页面时
// 看到的"进行中"行真正还在跑。
//
// 返回值：被回填的行数。
func (s *Store) SweepOrphans(maxStaleMinutes int) (int, error) {
	if s == nil {
		return 0, nil
	}
	if maxStaleMinutes <= 0 {
		maxStaleMinutes = 60
	}
	cutoff := time.Now().Add(-time.Duration(maxStaleMinutes) * time.Minute).UnixMilli()
	res, err := s.db.Exec(`
		UPDATE task_traces
		SET end_ms        = created_at_ms,
		    duration_ms   = 0,
		    success       = 0,
		    error_message = '进程异常退出，span 未正常关闭'
		WHERE end_ms = 0 AND created_at_ms < ?
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("sweep orphans: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// RetagTrace 同步把某 traceId 下所有 span 的 trace_type / task_id / run_seq
// 改写到新值。用于扫描完成后并入上传 trace 的场景，详见 tracer.RetagTrace 注释。
func (s *Store) RetagTrace(traceId, newTraceType, newTaskId string, newRunSeq int) error {
	if s == nil || traceId == "" {
		return nil
	}
	_, err := s.db.Exec(`
		UPDATE task_traces
		SET trace_type = ?, task_id = ?, run_seq = ?
		WHERE trace_id = ?
	`, newTraceType, newTaskId, newRunSeq, traceId)
	if err != nil {
		return fmt.Errorf("retag trace: %w", err)
	}
	return nil
}

// ReparentSpan 同步改写单条 span 的 parent_span_id。
func (s *Store) ReparentSpan(spanId, newParentSpanId string) error {
	if s == nil || spanId == "" {
		return nil
	}
	_, err := s.db.Exec(`
		UPDATE task_traces
		SET parent_span_id = ?
		WHERE span_id = ?
	`, newParentSpanId, spanId)
	if err != nil {
		return fmt.Errorf("reparent span: %w", err)
	}
	return nil
}

// CompactIfLarge 启动时调用一次：先 wal_checkpoint(TRUNCATE) 把 wal 合并回
// 主库（同时把 wal 文件清零），再判断主库大小是否超阈值。超过则同步 VACUUM
// 真正释放给 OS。
//
// 之所以先 checkpoint 再看 size：
//   - 频繁删除后 wal 可能膨胀到几百 MB 而主库还小，单看主库 size 会误判
//   - SQLite 默认在 wal 达到 1000 页（约 4 MB）时自动 checkpoint，但被反复
//     删 + 写后这个时机不可靠；这里启动时强制对齐一次
//
// 整个 Compact 操作同步执行，会阻塞 Init。在 GB 级数据库上 VACUUM 可能耗时
// 几秒到十几秒，用户启动客户端时能感知；这是"启动一次性代价 ↔ 之后磁盘干净"
// 的合理折中。返回 (originalSize, finalSize, vacuumed, err)，由调用方决定是否打日志。
func (s *Store) CompactIfLarge(thresholdMB int) (originalBytes, finalBytes int64, vacuumed bool, err error) {
	if s == nil {
		return 0, 0, false, nil
	}

	// 1) 先 checkpoint（TRUNCATE 模式：把 wal 内容合并回主库后清零 wal 文件）
	// 即使没超阈值，也建议每次启动做一次，让磁盘占用更"诚实"。
	if _, cerr := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); cerr != nil {
		// checkpoint 失败不致命：旧的 wal 数据仍在，下次启动还会再尝试
		log.Printf("[tracer] 启动 wal_checkpoint 失败: %v", cerr)
	}

	// 2) 现在再读主库大小（wal 已清空，size 反映真实数据占用）
	originalBytes = dbFileSize(s.dbPath)
	finalBytes = originalBytes

	if thresholdMB <= 0 {
		return
	}
	threshold := int64(thresholdMB) * 1024 * 1024
	if originalBytes <= threshold {
		return
	}

	// 3) VACUUM 整库 —— 重写主库文件，释放所有 free 页给 OS
	if _, vacErr := s.db.Exec(`VACUUM`); vacErr != nil {
		err = fmt.Errorf("vacuum failed: %w", vacErr)
		return
	}
	vacuumed = true
	finalBytes = dbFileSize(s.dbPath)
	return
}
