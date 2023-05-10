package impl

import (
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
		panic(err)
	}
	if isExits {
		fmt.Print("File exits, no need to download\n")
		return
	}
	fmt.Print("Start downloading...\n")
	if err := utils.CreateFile(d.filePath); err != nil {
		panic(err)
	}
	canPartial, err := d.checkServerSupportPartialReq()
	if err != nil {
		panic(err)
	}
	if canPartial {
		d.partialDownload()
	} else {
		d.fullDownload()
	}
	// 校验文件完整性
	err = d.checkIntegrity()
	if err != nil {
		panic(err)
	}
	fmt.Print("Done\n")
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

func (d *Downloader) fullDownload() {
	// 发送http请求
	resp, err := http.Get(d.url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	// 写入文件
	file, err := os.OpenFile(d.filePath, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	_, err = io.Copy(io.MultiWriter(file, d.bar), resp.Body)
	if err != nil {
		panic(err)
	}
}

func (d *Downloader) partialDownload() {
	var offset int64 = 0
	partSize := d.fileSize / int64(d.concurrencyN)
	wg.Add(d.concurrencyN)
	for i := 0; i < d.concurrencyN; i++ {
		go func(i int, offset int64) {
			defer wg.Done()
			if err := d.partialDownloadImpl(offset, partSize, i); err != nil {
				panic(err)
			}
		}(i, offset)
		offset += partSize
	}
	wg.Wait()
	d.mergeFile()
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
	defer resp.Body.Close()
	return d.partialWriteFile(offset, resp, i)
}

func (d *Downloader) partialWriteFile(startPos int64, resp *http.Response, i int) error {
	tmpFile, err := os.OpenFile(fmt.Sprintf("%s.tmp.%d", d.filePath, i), os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		panic(err)
	}
	defer tmpFile.Close()
	_, err = io.Copy(io.MultiWriter(tmpFile, d.bar), resp.Body)
	return err
}

func (d *Downloader) mergeFile() {
	file, err := os.OpenFile(d.filePath, os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	for i := 0; i < d.concurrencyN; i++ {
		tmpFile, err := os.Open(fmt.Sprintf("%s.tmp.%d", d.filePath, i))
		if err != nil {
			panic(err)
		}
		io.Copy(file, tmpFile)
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}
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
