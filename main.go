package main

import (
	"fmt"
	"hlsdump/dumper"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Printf("usage: %s url output_dir\n", os.Args[0])
		return
	}

	dumper := dumper.New(os.Args[1], os.Args[2])
	dumper.Start()
}
