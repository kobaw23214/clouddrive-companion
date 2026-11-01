package preselect

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 测试目录结构（t.TempDir() 下）：
//   root/
//     full/        a.txt, b.txt, sub/c.txt   —— 列表全覆盖 → 应折叠
//     partial/     x.txt, y.txt              —— 列表只给部分 → 不折叠
//     loose.txt                              —— 散文件
//     deep/        d1/d2/f.txt               —— 嵌套全覆盖 → 折叠到 deep
func setupTree(t *testing.T) (rootSlash string, paths map[string]string) {
	t.Helper()
	root := t.TempDir()
	mk := func(rel string) {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{
		"full/a.txt", "full/b.txt", "full/sub/c.txt",
		"partial/x.txt", "partial/y.txt",
		"loose.txt",
		"deep/d1/d2/f.txt",
	} {
		mk(rel)
	}

	rootSlash = filepath.ToSlash(root)
	paths = map[string]string{}
	for _, rel := range []string{
		"full/a.txt", "full/b.txt", "full/sub/c.txt",
		"partial/x.txt", "partial/y.txt",
		"loose.txt", "deep/d1/d2/f.txt",
	} {
		paths[rel] = rootSlash + "/" + rel
	}
	return rootSlash, paths
}

func dirPaths(r *Result) []string {
	var out []string
	for _, d := range r.Dirs {
		out = append(out, d.Path)
	}
	sort.Strings(out)
	return out
}

func findDir(r *Result, path string) *DirEntry {
	for i := range r.Dirs {
		if r.Dirs[i].Path == path {
			return &r.Dirs[i]
		}
	}
	return nil
}

// 列表完整覆盖 full/ + 一个散文件 → full 折叠（文件数 3），散文件保留
func TestCollapseFullDir(t *testing.T) {
	_, p := setupTree(t)
	res := Collapse([]string{p["full/a.txt"], p["full/b.txt"], p["full/sub/c.txt"], p["loose.txt"]})

	d := findDir(res, dirOf(t, p["full/a.txt"]))
	if d == nil {
		t.Fatalf("full 目录应被折叠, dirs=%v files=%v", dirPaths(res), res.Files)
	}
	if d.FileCount != 3 {
		t.Fatalf("full 文件数应为 3, 实际 %d", d.FileCount)
	}
	if len(res.Files) != 1 || res.Files[0] != p["loose.txt"] {
		t.Fatalf("散文件应只剩 loose.txt, 实际 %v", res.Files)
	}
	if len(res.Missing) != 0 {
		t.Fatalf("不应有 missing, 实际 %v", res.Missing)
	}
}

// 列表只覆盖 full/ 的部分文件（缺 sub/c.txt）→ 任何目录都不折叠，全部按散文件返回
func TestAdditionBlocksCollapse(t *testing.T) {
	_, p := setupTree(t)
	res := Collapse([]string{p["full/a.txt"], p["full/b.txt"]})

	if len(res.Dirs) != 0 {
		t.Fatalf("部分覆盖不应折叠, dirs=%v", dirPaths(res))
	}
	if len(res.Files) != 2 {
		t.Fatalf("应返回 2 个散文件, 实际 %v", res.Files)
	}
}

// 不存在的路径进 Missing；其余完整目录照常折叠
func TestMissingReported(t *testing.T) {
	root, p := setupTree(t)
	gone := root + "/gone/ghost.txt"
	res := Collapse([]string{p["full/a.txt"], p["full/b.txt"], p["full/sub/c.txt"], gone})

	if len(res.Missing) != 1 || res.Missing[0] != gone {
		t.Fatalf("gone.txt 应进 missing, 实际 %v", res.Missing)
	}
	if findDir(res, dirOf(t, p["full/a.txt"])) == nil {
		t.Fatalf("其余完整目录应照常折叠, dirs=%v", dirPaths(res))
	}
}

// 输入覆盖 root 下所有文件 → 折叠发生在最上层（可能是 root 的祖先——
// t.TempDir() 外层目录只含我们的文件，同样满足"全部覆盖"），显示最精简
func TestCollapseTopmost(t *testing.T) {
	root, p := setupTree(t)
	res := Collapse([]string{
		p["full/a.txt"], p["full/b.txt"], p["full/sub/c.txt"],
		p["partial/x.txt"], p["partial/y.txt"],
		p["loose.txt"], p["deep/d1/d2/f.txt"],
	})

	if len(res.Dirs) != 1 {
		t.Fatalf("应折叠成唯一目录项, dirs=%v files=%v", dirPaths(res), res.Files)
	}
	d := res.Dirs[0]
	if !strings.HasPrefix(root, d.Path) {
		t.Fatalf("折叠结果应是 root 或其祖先, dir=%s root=%s", d.Path, root)
	}
	if d.FileCount != 7 {
		t.Fatalf("文件数应为 7, 实际 %d", d.FileCount)
	}
	if len(res.Files) != 0 {
		t.Fatalf("不应有散文件, 实际 %v", res.Files)
	}
}

// 嵌套目录 deep/d1/d2 完整覆盖 → 折叠到 deep（而不是 d2），深层祖先不重复出现
func TestNestedCollapseDeepestTop(t *testing.T) {
	root, p := setupTree(t)
	res := Collapse([]string{p["deep/d1/d2/f.txt"]})

	dirs := dirPaths(res)
	if len(dirs) != 1 || dirs[0] != root+"/deep" {
		t.Fatalf("应只折叠出 deep 一层, dirs=%v", dirs)
	}
}

// 分隔符混用（\ 和 /）、尾部斜杠、重复输入 → 去重后正常折叠
func TestNormalizeAndDedupe(t *testing.T) {
	_, p := setupTree(t)
	win := func(s string) string { return strings.ReplaceAll(s, "/", "\\") }
	res := Collapse([]string{
		p["full/a.txt"], win(p["full/a.txt"]), // 同一路径两种分隔符
		win(p["full/b.txt"]),
		p["full/sub/c.txt"] + "/", // 文件带尾部斜杠
		"  " + p["full/a.txt"] + "  ", // 前后空格
	})

	if len(res.Files) != 0 {
		t.Fatalf("去重后不应有散文件, files=%v", res.Files)
	}
	d := findDir(res, dirOf(t, p["full/a.txt"]))
	if d == nil {
		t.Fatalf("去重后应正常折叠, dirs=%v", dirPaths(res))
	}
	if res.Received != 5 {
		t.Fatalf("received 应为去重前的 5, 实际 %d", res.Received)
	}
}

// 浏览器显式传目录 → 直接作为目录项；被该目录覆盖的散文件不重复出现
func TestExplicitDirInput(t *testing.T) {
	root, p := setupTree(t)
	res := Collapse([]string{root + "/full", p["full/a.txt"], p["loose.txt"]})

	d := findDir(res, root+"/full")
	if d == nil {
		t.Fatalf("显式目录应保留, dirs=%v", dirPaths(res))
	}
	if d.FileCount != 3 {
		t.Fatalf("显式目录文件数应为 3, 实际 %d", d.FileCount)
	}
	if len(res.Files) != 1 || res.Files[0] != p["loose.txt"] {
		t.Fatalf("full/a.txt 应被显式目录覆盖，只剩 loose.txt, files=%v", res.Files)
	}
}

// 空输入 / 全是 missing → 不折叠、不报错
func TestEmptyAndAllMissing(t *testing.T) {
	res := Collapse(nil)
	if res.Received != 0 || len(res.Dirs) != 0 || len(res.Files) != 0 {
		t.Fatalf("空输入应返回空结果: %+v", res)
	}

	res = Collapse([]string{"Z:/no/such/file.txt", "", "   "})
	if len(res.Missing) != 1 || len(res.Dirs) != 0 || len(res.Files) != 0 {
		t.Fatalf("全部 missing: %+v", res)
	}
}

func dirOf(t *testing.T, slashFile string) string {
	t.Helper()
	i := strings.LastIndex(slashFile, "/")
	if i < 0 {
		t.Fatalf("bad path %s", slashFile)
	}
	return slashFile[:i]
}
