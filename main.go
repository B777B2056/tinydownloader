package main

import (
	"flag"
	"runtime"
	"tinydownloader/impl"
)

var url string
var savePath string
var concurrencyN int

func parseCmd() {
	flag.StringVar(&url, "u", "", "target url")
	flag.StringVar(&savePath, "p", ".", "file save path")
	flag.IntVar(&concurrencyN, "n", runtime.NumCPU(), "concurrency level")
	flag.Parse()
}

func main() {
	parseCmd()
	worker := impl.NewDownloader(url, savePath, concurrencyN)
	worker.Start()
}
