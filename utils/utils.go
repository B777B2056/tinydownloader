package utils

import (
	"os"
	"strings"
)

func IsFileExits(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func CreateFile(path string) error {
	// 建立文件夹
	if tmp := strings.Split(path, "/"); len(tmp) > 1 {
		dir := tmp[len(tmp)-2]
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return err
		}
	}
	// 建立文件
	f, err := os.Create(path)
	f.Close()
	return err
}
