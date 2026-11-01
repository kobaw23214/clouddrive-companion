// Package preselect 实现"选文件追加"场景的预选恢复。
//
// 背景：浏览器端保存了用户上次确认上传的文件列表（每项含 absolutePath），
// 用户点"追加"时浏览器把这份列表 POST 给客户端（/reselect），客户端打开
// 文件选择器并把这些文件重新勾选上，用户补选后一起提交。
//
// 本包负责把这份（可能成千上万的）文件路径列表处理成 FileSelector 能直接
// 消费的"预选结果"：
//  1. 规范化：统一 slash 分隔、去重、Windows 下大小写不敏感比较；
//  2. 存在性检查：stat 每个路径，不存在的进 Missing 上报给浏览器；
//  3. 目录折叠（显示最小化）：若某目录当前磁盘上的全部常规文件都在列表里，
//     则用目录项替代这一堆文件——与 FileSelector "勾选目录 = 全选其下文件"
//     的原生语义完全一致，恢复后走现有扫描自然重新展开。
//
// 契约上浏览器只需给一组 absolutePath 字符串，fileName/fileSize/relativePath
// 等字段客户端恢复时自行从磁盘获取，不依赖浏览器传递。
package preselect

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// DirEntry 一个被折叠出来的目录项：表示"该目录被整体选中"。
type DirEntry struct {
	Path      string `json:"path"`      // 目录路径（slash 形式，无尾部分隔符）
	FileCount int    `json:"fileCount"` // 恢复时目录下的常规文件数（磁盘实况，仅用于展示）
}

// Result 预选恢复结果，同时用作：
//   - Wails 绑定 GetPendingPreselect 的返回值（FileSelector 据此勾选，需要完整 dirs/files）；
//   - /reselect HTTP 响应的数据来源（服务端回包时 files 只回数量，不回明细）。
type Result struct {
	Dirs     []DirEntry `json:"dirs"`     // 折叠出的目录项（含浏览器显式传入的目录）
	Files    []string   `json:"files"`    // 未能被目录折叠的散文件（slash 形式）
	Missing  []string   `json:"missing"`  // 已不存在或不可读的输入路径（slash 形式）
	Received int        `json:"received"` // 收到的输入条数（去重前）
}

// caseInsensitive 路径比较是否大小写不敏感（仅 Windows）。
var caseInsensitive = runtime.GOOS == "windows"

// errWalkAborted visit 回调要求提前终止时用的哨兵错误。
var errWalkAborted = errors.New("walk aborted")

// Collapse 把浏览器带回的文件路径列表处理成预选恢复结果。
// 输入只需 absolutePath 字符串（\ 或 / 分隔均可），其余字段一律忽略。
func Collapse(inputs []string) *Result {
	result := &Result{Received: len(inputs)}

	// fileSet: fold(路径) -> 展示用路径（首次出现的原始大小写形式）
	fileSet := make(map[string]string)
	var explicitDirs []DirEntry
	seen := make(map[string]bool)

	for _, raw := range inputs {
		p := normalizePath(raw)
		if p == "" {
			continue
		}
		key := fold(p)
		if seen[key] {
			continue
		}
		seen[key] = true

		info, err := os.Stat(filepath.FromSlash(p))
		if err != nil {
			result.Missing = append(result.Missing, p)
			continue
		}
		if info.IsDir() {
			// 浏览器显式传入目录：按"整个目录被选中"处理，文件数仅作展示。
			count, _ := countRegularFiles(p)
			explicitDirs = append(explicitDirs, DirEntry{Path: p, FileCount: count})
			continue
		}
		fileSet[key] = p
	}

	// 嵌套的显式目录去重：父目录已选时丢掉子目录，避免提交后重复扫描。
	explicitDirs = dropNestedDirs(explicitDirs)

	// 散文件里被显式目录覆盖的移除（同理避免重复）。
	var dirPrefixes []string
	for _, d := range explicitDirs {
		dirPrefixes = append(dirPrefixes, fold(d.Path)+"/")
	}
	for key := range fileSet {
		if hasAnyPrefix(key, dirPrefixes) {
			delete(fileSet, key)
		}
	}

	// 目录折叠：自顶向下（路径浅的优先）尝试每个候选祖先目录，
	// 磁盘上的全部常规文件都在输入集合里 → 折叠成目录项。
	coveredPrefixes := dirPrefixes // 已被目录覆盖的 fold 前缀（含显式目录）
	for _, cand := range ancestorCandidates(fileSet) {
		if hasAnyPrefix(fold(cand)+"/", coveredPrefixes) {
			continue // 已被上层折叠目录覆盖
		}
		count, ok := dirFullyInSet(cand, fileSet)
		if !ok {
			continue
		}
		result.Dirs = append(result.Dirs, DirEntry{Path: cand, FileCount: count})
		coveredPrefixes = append(coveredPrefixes, fold(cand)+"/")
	}
	result.Dirs = append(result.Dirs, explicitDirs...)

	// 剩下的就是没被任何目录折叠的散文件。
	for key, display := range fileSet {
		if !isUnderAnyDir(key, coveredPrefixes) {
			result.Files = append(result.Files, display)
		}
	}

	sort.Slice(result.Dirs, func(i, j int) bool { return fold(result.Dirs[i].Path) < fold(result.Dirs[j].Path) })
	sort.Slice(result.Files, func(i, j int) bool { return fold(result.Files[i]) < fold(result.Files[j]) })
	sort.Strings(result.Missing)
	return result
}

// normalizePath 规范化输入路径：统一为 slash 分隔、去掉尾部分隔符、解析 . / ..。
// 无法规出有效路径时返回空串。保留原始大小写（大小写不敏感只用于比较键）。
func normalizePath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean(p)
	// path.Clean("E:/") 会得到 "E:"，盘符根目录需要补回 "/"。
	if len(p) == 2 && p[1] == ':' {
		p += "/"
	}
	if p == "." || p == "/" {
		return ""
	}
	// 去掉尾部分隔符（盘符根目录 "E:/" 保留）。
	if len(p) > 3 && strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// fold 返回比较用键：Windows 下路径大小写不敏感。
func fold(p string) string {
	if caseInsensitive {
		return strings.ToLower(p)
	}
	return p
}

// parentOf 返回 slash 路径的父目录；到盘符根（或根）后返回空串表示没有父级。
func parentOf(p string) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return ""
	}
	parent := p[:i]
	if len(parent) == 2 && parent[1] == ':' {
		parent += "/" // "E:" -> "E:/"（盘符根）
	}
	if parent == p {
		return ""
	}
	return parent
}

// ancestorCandidates 收集所有文件的祖先目录，按深度从浅到深排序（同级按字典序，
// 保证结果确定性）。浅的优先可以让折叠尽量发生在上层目录，显示最精简。
func ancestorCandidates(fileSet map[string]string) []string {
	set := make(map[string]bool)
	for _, display := range fileSet {
		for parent := parentOf(display); parent != ""; parent = parentOf(parent) {
			set[parent] = true
		}
	}
	candidates := make([]string, 0, len(set))
	for c := range set {
		candidates = append(candidates, c)
	}
	sort.Slice(candidates, func(i, j int) bool {
		di, dj := strings.Count(candidates[i], "/"), strings.Count(candidates[j], "/")
		if di != dj {
			return di < dj
		}
		return fold(candidates[i]) < fold(candidates[j])
	})
	return candidates
}

// dirFullyInSet 判断目录 dir 当前磁盘上的全部常规文件是否都在 fileSet 里。
// 全部在且至少有一个文件时返回 (文件数, true)。目录不存在/不可读/为空返回 false。
func dirFullyInSet(dir string, fileSet map[string]string) (int, bool) {
	count := 0
	allIn := true
	completed, err := walkRegularFiles(filepath.FromSlash(dir), func(slashPath string) bool {
		if _, in := fileSet[fold(slashPath)]; !in {
			allIn = false
			return false // 发现集合外的文件 → 提前终止
		}
		count++
		return true
	})
	if err != nil || !completed || !allIn || count == 0 {
		return 0, false
	}
	return count, true
}

// countRegularFiles 统计目录下的常规文件数（用于显式目录项的展示）。
func countRegularFiles(dir string) (int, error) {
	count := 0
	_, err := walkRegularFiles(filepath.FromSlash(dir), func(string) bool {
		count++
		return true
	})
	return count, err
}

// walkRegularFiles 以与 internal/utils/file.go scanDir 相同的口径递归枚举 root 下的
// 常规文件：只收集 regular 文件（符号链接、管道等非常规文件跳过），读不了的子目录
// 跳过并继续，保证与"勾选目录后扫描展开"的结果一致。
//
// visit 返回 false 时提前终止（completed=false）。root 本身不存在或不可读时返回错误。
func walkRegularFiles(root string, visit func(slashPath string) (cont bool)) (completed bool, err error) {
	aborted := false
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == root {
				return walkErr // root 不可读 → 整体失败
			}
			if d != nil && d.IsDir() {
				return filepath.SkipDir // 与 scanDir 一致：跳过读不了的子目录
			}
			return nil // 与 scanDir 一致：stat 失败的条目跳过
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // 与 scanDir 一致：只收常规文件
		}
		if !visit(filepath.ToSlash(p)) {
			aborted = true
			return errWalkAborted
		}
		return nil
	})
	if errors.Is(err, errWalkAborted) {
		err = nil
	}
	return !aborted, err
}

// dropNestedDirs 去掉被其他显式目录包含的目录，避免提交后重复扫描。
func dropNestedDirs(dirs []DirEntry) []DirEntry {
	sorted := make([]DirEntry, len(dirs))
	copy(sorted, dirs)
	sort.Slice(sorted, func(i, j int) bool { return fold(sorted[i].Path) < fold(sorted[j].Path) })

	kept := make([]DirEntry, 0, len(sorted))
	var prefixes []string
	for _, d := range sorted {
		if hasAnyPrefix(fold(d.Path)+"/", prefixes) {
			continue
		}
		kept = append(kept, d)
		prefixes = append(prefixes, fold(d.Path)+"/")
	}
	return kept
}

// hasAnyPrefix 判断 key 是否以任一前缀开头（前缀需自带尾部 "/"，调用方保证）。
func hasAnyPrefix(key string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// isUnderAnyDir 判断路径 fold key 是否位于任一目录前缀之下。
func isUnderAnyDir(key string, dirPrefixes []string) bool {
	return hasAnyPrefix(key+"/", dirPrefixes)
}
