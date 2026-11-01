package server

import (
	"bytes"
	"clouddrive-companion-withWails2/internal/config"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// 应用日志（clouddrive-companion-YYYY-MM-DD.log）的只读访问层
//
// 设计要点：
//   - 文件位于 AppSupportDir 根目录，命名 clouddrive-companion-YYYY-MM-DD.log
//   - 读取按"行倒序"：从文件末尾开始按块反向扫描，避免把整个文件读入内存
//   - 支持简单关键字过滤（大小写不敏感）
//   - 单次最多返回 size 条；前端通过 offset/size 分页

var appLogFileNameRe = regexp.MustCompile(`^clouddrive-companion-(\d{4}-\d{2}-\d{2})\.log$`)

// AppLogFileInfo 单个日志文件元信息
type AppLogFileInfo struct {
	Name       string `json:"name"`
	Date       string `json:"date"`
	Size       int64  `json:"size"`
	ModifiedMs int64  `json:"modifiedMs"`
}

// AppLogPage 分页结果（按行倒序）
type AppLogPage struct {
	Name           string   `json:"name"`
	TotalSize      int64    `json:"totalSize"`      // 文件总字节数
	ReturnedLines  int      `json:"returnedLines"`  // 本次返回的行数
	HasMore        bool     `json:"hasMore"`        // 是否还有更早的内容
	NextOffset     int64    `json:"nextOffset"`     // 下次请求传这个 offset（向更早的方向继续读）
	Lines          []string `json:"lines"`          // 倒序行（最新在前）
}

// ListAppLogFiles 列出 AppSupportDir 下所有日志文件，按日期倒序
func ListAppLogFiles() []AppLogFileInfo {
	dir := config.AppSupportDir
	if dir == "" {
		return []AppLogFileInfo{}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []AppLogFileInfo{}
	}
	out := make([]AppLogFileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := appLogFileNameRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, AppLogFileInfo{
			Name:       e.Name(),
			Date:       m[1],
			Size:       info.Size(),
			ModifiedMs: info.ModTime().UnixMilli(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	return out
}

// ReadAppLogFile 按行从文件末尾开始倒序读取 size 行（可选关键字过滤）
//
//	name:    日志文件名（必须匹配命名规则；不允许路径分隔符）
//	offset:  起始字节偏移（首次传 0 表示从文件末尾开始；后续传上一次返回的 nextOffset）
//	size:    本次最多返回的行数（>=1）
//	keyword: 可选；非空时仅返回包含该子串的行（大小写不敏感）
func ReadAppLogFile(name string, offset int64, size int, keyword string) (AppLogPage, error) {
	empty := AppLogPage{Name: name, Lines: []string{}}
	if name == "" || strings.ContainsAny(name, "/\\") {
		return empty, fmt.Errorf("invalid log file name")
	}
	if !appLogFileNameRe.MatchString(name) {
		return empty, fmt.Errorf("not an app log file name")
	}
	if size < 1 {
		size = 200
	}

	dir := config.AppSupportDir
	if dir == "" {
		return empty, fmt.Errorf("app support dir not configured")
	}
	path := filepath.Join(dir, name)
	f, err := os.Open(path)
	if err != nil {
		return empty, fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return empty, fmt.Errorf("stat log: %w", err)
	}
	totalSize := stat.Size()
	empty.TotalSize = totalSize

	// 起始位置：offset==0 表示从文件末尾开始
	cursor := offset
	if cursor <= 0 || cursor > totalSize {
		cursor = totalSize
	}

	const blockSize = int64(64 * 1024)
	keywordLower := strings.ToLower(keyword)

	var lines []string
	var leftover []byte // 上一次块切下来、跨边界的末尾片段（实际是文件中靠后的内容）

	for cursor > 0 && len(lines) < size {
		readSize := blockSize
		if readSize > cursor {
			readSize = cursor
		}
		newCursor := cursor - readSize
		buf := make([]byte, readSize)
		if _, err := f.ReadAt(buf, newCursor); err != nil && err != io.EOF {
			return empty, fmt.Errorf("read at: %w", err)
		}

		// 把上次留下来的尾部接到本次块的末尾（保持文件原顺序）
		if len(leftover) > 0 {
			combined := make([]byte, 0, len(buf)+len(leftover))
			combined = append(combined, buf...)
			combined = append(combined, leftover...)
			buf = combined
			leftover = nil
		}

		// 在 buf 中按 \n 切分；最早的一段（buf[:firstNL]）可能是不完整的，
		// 需要保留到下一轮和更早的字节合并
		firstNL := bytes.IndexByte(buf, '\n')
		var partial []byte
		var rest []byte
		if firstNL == -1 {
			// 整段都没换行，全部作为 partial 保留
			partial = buf
		} else {
			partial = buf[:firstNL]
			rest = buf[firstNL+1:]
		}

		// 如果还会继续往前读，就把当前 partial 留给下一轮；
		// 否则把它当成一行（文件起始可能没换行符就直接是日志首行）
		if newCursor > 0 {
			leftover = make([]byte, len(partial))
			copy(leftover, partial)
		} else {
			// 已到文件头，把 partial 作为最早一行处理
			rest = append(append([]byte{}, partial...), append([]byte{'\n'}, rest...)...)
		}

		// rest 里是若干完整行，按倒序追加到结果
		// 用 SplitAfter 保留分隔，然后处理 \r\n
		for _, l := range reverseSplitLines(rest) {
			if keywordLower != "" && !strings.Contains(strings.ToLower(l), keywordLower) {
				continue
			}
			lines = append(lines, l)
			if len(lines) >= size {
				break
			}
		}

		cursor = newCursor
	}

	// 如果尚未到达文件头但已经收够了 size 行，nextOffset = cursor（包含 leftover 的字节区间起点）
	// 因为 leftover 还没被消费，需要把它的字节数也算回 cursor
	nextOffset := cursor + int64(len(leftover))
	hasMore := nextOffset > 0 && len(lines) >= size

	return AppLogPage{
		Name:          name,
		TotalSize:     totalSize,
		ReturnedLines: len(lines),
		HasMore:       hasMore,
		NextOffset:    nextOffset,
		Lines:         lines,
	}, nil
}

// reverseSplitLines 把 buf 按换行切成行，返回"倒序"的字符串切片（最末一行在前）
func reverseSplitLines(buf []byte) []string {
	if len(buf) == 0 {
		return nil
	}
	// 末尾如果是换行符，去掉避免产生空行
	end := len(buf)
	for end > 0 && (buf[end-1] == '\n' || buf[end-1] == '\r') {
		end--
	}
	if end == 0 {
		return nil
	}
	parts := bytes.Split(buf[:end], []byte{'\n'})
	out := make([]string, 0, len(parts))
	for i := len(parts) - 1; i >= 0; i-- {
		s := string(bytes.TrimRight(parts[i], "\r"))
		out = append(out, s)
	}
	return out
}

// DeleteAppLogFiles 批量删除应用日志文件
func DeleteAppLogFiles(names []string) error {
	dir := config.AppSupportDir
	if dir == "" {
		return fmt.Errorf("app support dir not configured")
	}
	var errs []string
	for _, name := range names {
		if name == "" || strings.ContainsAny(name, "/\\") || !appLogFileNameRe.MatchString(name) {
			errs = append(errs, fmt.Sprintf("invalid file name: %s", name))
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("remove %s: %v", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
