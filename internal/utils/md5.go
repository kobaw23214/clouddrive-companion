package utils

import (
	"clouddrive-companion-withWails2/internal/config"
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

// CalculateFileMD5 计算文件 MD5 值
func CalculateFileMD5(filePath string) (string, error) {
	if config.AppConfig.EnableMD5Cache {
		if cached, ok := config.MD5Cache.Load(filePath); ok {
			return cached.(string), nil
		}
	}

	file, err := os.Open(filepath.FromSlash(filePath))
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	md5String := hex.EncodeToString(hash.Sum(nil))

	if config.AppConfig.EnableMD5Cache {
		config.MD5Cache.Store(filePath, md5String)
	}

	return md5String, nil
}
