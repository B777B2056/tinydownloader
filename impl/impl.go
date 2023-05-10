package impl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"tinydownloader/utils"

	"github.com/schollz/progressbar/v3"
)

var wg sync.WaitGroup

type Downloader struct {
	url          string
	concurrencyN int
	filePath     string
	fileSize     int64
	bar          *progressbar.ProgressBar
}

func NewDownloader(url string, savePath string, concurrencyN int) *Downloader {
	d := Downloader{
		url:          url,
		concurrencyN: concurrencyN,
	}
	tmp := strings.Split(url, "/")
	fileName := tmp[len(tmp)-1]
	if savePath[len(savePath)-1] != '/' {
		d.filePath = savePath + "/" + fileName
	} else {
		d.filePath = savePath + fileName
	}
	return &d
}

func (d *Downloader) Start() {
	isExits, err := utils.IsFileExits(d.filePath)
	if err != nil {
		fmt.Printf("%s", err.Error())
		return
	}
	if isExits {
		fmt.Print("File exits, no need to download\n")
		return
	}
	fmt.Print("Start downloading...\n")
	if err := utils.CreateFile(d.filePath); err != nil {
		fmt.Printf("Create file failed: %s", err.Error())
		return
	}
	canPartial, err := d.checkServerSupportPartialReq()
	if err != nil {
		fmt.Printf("HTTP HEAD method failed: %s", err.Error())
		return
	}
	if canPartial {
		if err := d.partialDownload(); err != nil {
			fmt.Printf("Download failed: %s", err.Error())
			return
		}
	} else {
		if err := d.fullDownload(); err != nil {
			fmt.Printf("Download failed: %s", err.Error())
			return
		}
	}
	// 校验文件完整性
	if err := d.checkIntegrity(); err != nil {
		fmt.Printf("Create file failed: %s", err.Error())
	} else {
		fmt.Print("Done\n")
	}
}

func (d *Downloader) checkServerSupportPartialReq() (bool, error) {
	resp, err := http.Head(d.url)
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusOK {
		return false, errors.New(resp.Status)
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		return false, nil
	}
	d.fileSize = resp.ContentLength
	d.bar = progressbar.DefaultBytes(
		d.fileSize,
		"downloading",
	)
	return true, nil
}

func (d *Downloader) fullDownload() error {
	// 发送http请求
	resp, err := http.Get(d.url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code is %d", resp.StatusCode)
	}
	// 写入文件
	file, err := os.OpenFile(d.filePath, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(io.MultiWriter(file, d.bar), resp.Body)
	return err
}

func (d *Downloader) partialDownload() error {
	// 错误处理相关
	ctx, stop := context.WithCancel(context.Background())
	errChan := make(chan error, 1)
	defer stop()
	// 偏移量
	var offset int64 = 0
	// 每个go程所分配到的文件大小
	partSize := d.fileSize / int64(d.concurrencyN)
	wg.Add(d.concurrencyN)
	for i := 0; i < d.concurrencyN; i++ {
		go func(i int, offset int64) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err := d.partialDownloadImpl(offset, partSize, i); err != nil {
				select {
				case errChan <- fmt.Errorf("partial download failed: %s", err.Error()):
				default:
				}
				stop()
			}
		}(i, offset)
		offset += partSize
	}
	wg.Wait()
	if ctx.Err() != nil {
		return <-errChan
	}
	// 合并各部分文件
	return d.mergeFile()
}

func (d *Downloader) partialDownloadImpl(offset, partSize int64, i int) error {
	req, err := http.NewRequest("GET", d.url, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Accept-Encoding", "identity")
	if i == d.concurrencyN-1 {
		req.Header.Add("Range", fmt.Sprintf("bytes=%d-", offset))
	} else {
		req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+partSize-1))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("status code is %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	return d.partialWriteFile(resp, i)
}

func (d *Downloader) partialWriteFile(resp *http.Response, i int) error {
	tmpFile, err := os.OpenFile(fmt.Sprintf("%s.tmp.%d", d.filePath, i), os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer tmpFile.Close()
	_, err = io.Copy(io.MultiWriter(tmpFile, d.bar), resp.Body)
	return err
}

func (d *Downloader) mergeFile() error {
	file, err := os.OpenFile(d.filePath, os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}
	defer file.Close()
	for i := 0; i < d.concurrencyN; i++ {
		tmpFile, err := os.Open(fmt.Sprintf("%s.tmp.%d", d.filePath, i))
		if err != nil {
			return err
		}
		_, err = io.Copy(file, tmpFile)
		tmpFile.Close()
		if err != nil {
			return err
		}
		os.Remove(tmpFile.Name())
	}
	return nil
}

func (d *Downloader) checkIntegrity() error {
	fi, err := os.Stat(d.filePath)
	if err != nil {
		return err
	}
	if fi.Size() != d.fileSize {
		return fmt.Errorf("file size not match: target size is %d bytes, but download %d bytes", d.fileSize, fi.Size())
	}
	return nil
}
