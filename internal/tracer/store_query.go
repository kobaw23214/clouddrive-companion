package tracer

import (
	"database/sql"
	"fmt"
	"strings"
)

// 本文件实现 Store 的只读查询与删除接口，handler 层调用。
// 所有 SQL 都用参数化绑定，防止注入。

// scanSpan 把单行 task_traces 解码为 Span。
func scanSpan(s rowScanner) (Span, error) {
	var sp Span
	var (
		errMsg sql.NullString
		extra  sql.NullString
	)
	var success int
	if err := s.Scan(
		&sp.ID, &sp.SpanID, &sp.ParentSpanID, &sp.TraceID, &sp.TaskID, &sp.RunSeq,
		&sp.TraceType, &sp.Stage, &sp.Filename, &sp.StartMs, &sp.EndMs, &sp.DurationMs,
		&success, &errMsg, &extra, &sp.CreatedAtMs,
	); err != nil {
		return Span{}, err
	}
	sp.Success = success != 0
	sp.ErrorMessage = errMsg.String
	sp.Extra = extra.String
	return sp, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

const baseSpanColumns = `id, span_id, parent_span_id, trace_id, task_id, run_seq,
       trace_type, stage, filename, start_ms, end_ms, duration_ms,
       success, error_message, extra, created_at_ms`

// GetTopSpans 返回某 trace 的所有顶层 span（parent_span_id == ''），
// 按 start_ms 升序。
func (s *Store) GetTopSpans(traceId string) ([]Span, error) {
	if s == nil || traceId == "" {
		return []Span{}, nil
	}
	rows, err := s.db.Query(
		`SELECT `+baseSpanColumns+` FROM task_traces
		 WHERE trace_id = ? AND parent_span_id = ''
		 ORDER BY start_ms ASC, id ASC`,
		traceId,
	)
	if err != nil {
		return nil, fmt.Errorf("query top spans: %w", err)
	}
	defer rows.Close()
	return collectSpans(rows)
}

// GetChildren 返回某 span 的直接子 span，按 start_ms 升序。
func (s *Store) GetChildren(parentSpanId string) ([]Span, error) {
	if s == nil || parentSpanId == "" {
		return []Span{}, nil
	}
	rows, err := s.db.Query(
		`SELECT `+baseSpanColumns+` FROM task_traces
		 WHERE parent_span_id = ?
		 ORDER BY start_ms ASC, id ASC`,
		parentSpanId,
	)
	if err != nil {
		return nil, fmt.Errorf("query children: %w", err)
	}
	defer rows.Close()
	return collectSpans(rows)
}

// GetAllSpans 返回某 trace 的所有 span（平铺），最多 limit 条。
//
// truncated 表示数据量超出 limit、调用方需要警示用户结果不完整。
func (s *Store) GetAllSpans(traceId string, limit int) (spans []Span, truncated bool, err error) {
	if s == nil || traceId == "" {
		return []Span{}, false, nil
	}
	if limit <= 0 {
		limit = 5000
	}
	// 多查一条用于判断是否截断
	rows, err := s.db.Query(
		`SELECT `+baseSpanColumns+` FROM task_traces
		 WHERE trace_id = ?
		 ORDER BY start_ms ASC, id ASC
		 LIMIT ?`,
		traceId, limit+1,
	)
	if err != nil {
		return nil, false, fmt.Errorf("query all spans: %w", err)
	}
	defer rows.Close()

	all, err := collectSpans(rows)
	if err != nil {
		return nil, false, err
	}
	if len(all) > limit {
		return all[:limit], true, nil
	}
	return all, false, nil
}

// GetSpan 按 spanId 获取单条 span，主要用于详情视图。
// 找不到返回 sql.ErrNoRows。
func (s *Store) GetSpan(spanId string) (Span, error) {
	if s == nil || spanId == "" {
		return Span{}, sql.ErrNoRows
	}
	row := s.db.QueryRow(
		`SELECT `+baseSpanColumns+` FROM task_traces WHERE span_id = ? LIMIT 1`,
		spanId,
	)
	return scanSpan(row)
}

// DeleteTrace 删除某 trace 的所有 span，返回删除条数。
func (s *Store) DeleteTrace(traceId string) (int, error) {
	if s == nil || traceId == "" {
		return 0, nil
	}
	res, err := s.db.Exec(`DELETE FROM task_traces WHERE trace_id = ?`, traceId)
	if err != nil {
		return 0, fmt.Errorf("delete trace: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ListTraces 按条件分页返回 trace 摘要。
//
// 实现要点：
//   - 用 GROUP BY trace_id 聚合
//   - has_failure 通过 SUM(CASE WHEN success=0 THEN 1 ELSE 0 END) > 0 判断（兼容 SQLite）
//   - 同一 trace 内 task_id / trace_type / run_seq 是常量；为兼容 SQLite 严格 GROUP BY 用 MAX 包一下
//   - 总数用单独的 SELECT COUNT(DISTINCT trace_id) 查询
func (s *Store) ListTraces(q ListTracesQuery) ([]TraceSummary, int, error) {
	if s == nil {
		return []TraceSummary{}, 0, nil
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	size := q.Size
	if size < 1 {
		size = 50
	} else if size > 200 {
		size = 200
	}
	offset := (page - 1) * size

	conds, args := buildListConditions(q)
	whereSQL := ""
	if len(conds) > 0 {
		whereSQL = " WHERE " + strings.Join(conds, " AND ")
	}

	// total: COUNT(DISTINCT trace_id)，filter 同样的 where
	var total int
	if err := s.db.QueryRow(
		`SELECT COUNT(DISTINCT trace_id) FROM task_traces`+whereSQL, args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count traces: %w", err)
	}
	if total == 0 {
		return []TraceSummary{}, 0, nil
	}

	listSQL := `
		SELECT trace_id,
		       MAX(task_id)         AS task_id,
		       MAX(run_seq)         AS run_seq,
		       MAX(trace_type)      AS trace_type,
		       MIN(start_ms)        AS start_ms,
		       MAX(end_ms)          AS end_ms,
		       MAX(end_ms) - MIN(start_ms) AS duration_ms,
		       COUNT(*)             AS span_count,
		       CASE WHEN SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) > 0
		            THEN 1 ELSE 0 END AS has_failure,
		       SUM(duration_ms)     AS total_self_duration_ms
		FROM task_traces` + whereSQL + `
		GROUP BY trace_id
		ORDER BY MIN(start_ms) DESC
		LIMIT ? OFFSET ?`
	args = append(args, size, offset)

	rows, err := s.db.Query(listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list traces: %w", err)
	}
	defer rows.Close()

	out := make([]TraceSummary, 0, size)
	for rows.Next() {
		var ts TraceSummary
		var hasFailureInt int
		if err := rows.Scan(
			&ts.TraceID, &ts.TaskID, &ts.RunSeq, &ts.TraceType,
			&ts.StartMs, &ts.EndMs, &ts.DurationMs,
			&ts.SpanCount, &hasFailureInt, &ts.TotalSelfDurationMs,
		); err != nil {
			return nil, 0, fmt.Errorf("scan trace summary: %w", err)
		}
		ts.HasFailure = hasFailureInt != 0
		out = append(out, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// buildListConditions 构造 ListTraces 的 WHERE 条件与对应参数。
func buildListConditions(q ListTracesQuery) ([]string, []interface{}) {
	var conds []string
	var args []interface{}

	if q.TraceType != "" {
		conds = append(conds, "trace_type = ?")
		args = append(args, q.TraceType)
	}
	if q.TaskID != "" {
		conds = append(conds, "task_id = ?")
		args = append(args, q.TaskID)
	}
	if q.FromMs > 0 {
		conds = append(conds, "start_ms >= ?")
		args = append(args, q.FromMs)
	}
	if q.ToMs > 0 {
		conds = append(conds, "start_ms <= ?")
		args = append(args, q.ToMs)
	}
	return conds, args
}

// collectSpans 把 *sql.Rows 一次性收集为 []Span。
func collectSpans(rows *sql.Rows) ([]Span, error) {
	out := make([]Span, 0, 64)
	for rows.Next() {
		sp, err := scanSpan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
