package main

import (
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"hlsdump/dumper"
	"hlsdump/pkg/logger"
	"os"
)

var (
	logdir  = flag.String("logdir", "", "log file directory, log to stdout if empty")
	timeout = flag.Int("timeout", 30, "download timeout")
	retry   = flag.Int("retry", 3, "retry count if failed")
)

func main() {
	flag.Parse()

	if len(flag.Args()) < 2 {
		fmt.Printf("usage: %s [-timeout=30] [-retry=3] url output_dir\n", os.Args[0])
		return
	}

	logfile := ""
	if logdir != nil && *logdir != "" {
		os.MkdirAll(*logdir, 0755)
		md5hash := md5.Sum([]byte(flag.Args()[0]))
		tmpname := hex.EncodeToString(md5hash[:])
		logfile = *logdir + "/" + tmpname + ".log"
		fmt.Printf("hlsdump logs to %s\n", logfile)
	}

	logger.Init(logfile, "info")

	cfg := dumper.Config{
		Url:     flag.Args()[0],
		Dir:     flag.Args()[1],
		Retry:   *retry,
		Timeout: *timeout,
	}
	dumper := dumper.New(&cfg)
	dumper.Start()
}
