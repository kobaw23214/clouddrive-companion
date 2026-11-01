// genconfig 输出一份"纯默认值"的 config.json。
//
// 为什么需要它：
//
//	安装包的 [Files] 段要求 build/bin/config.json 存在，而该目录被 .gitignore
//	忽略、wails build 也不生成它 —— 干净环境下打包会直接失败。更麻烦的是，
//	开发机上那份 config.json 往往指向本机后端、带着调好的端口，一旦被打进
//	安装包就会散给客户。
//
//	这里直接以 internal/config.DefaultConfig 为唯一来源生成，
//	既避免手写副本随代码漂移，也保证打包用的是干净默认值。
//
// 用法：
//
//	go run ./cmd/genconfig                     # 打印到标准输出
//	go run ./cmd/genconfig -o build/bin/config.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"clouddrive-companion-withWails2/internal/config"
)

func main() {
	out := flag.String("o", "", "输出文件路径（留空则打印到标准输出）")
	flag.Parse()

	data, err := json.MarshalIndent(config.DefaultConfig, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "序列化默认配置失败: %v\n", err)
		os.Exit(1)
	}
	data = append(data, '\n')

	if *out == "" {
		os.Stdout.Write(data)
		return
	}

	if dir := filepath.Dir(*out); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "创建目录失败: %v\n", err)
			os.Exit(1)
		}
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写入 %s 失败: %v\n", *out, err)
		os.Exit(1)
	}
	fmt.Printf("已写入默认配置: %s\n", *out)
}
