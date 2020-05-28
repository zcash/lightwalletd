// This program increments a given byte of a given file,
// to test data corruption detection -- BE CAREFUL!
package main

import (
	"fmt"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Println("usage:", os.Args[0], "file offset")
		os.Exit(1)
	}
	f, err := os.OpenFile(os.Args[1], os.O_RDWR, 0644)
	if err != nil {
		fmt.Println("open failed:", err)
		os.Exit(1)
	}
	offset, err := strconv.ParseInt(os.Args[2], 10, 64)
	if err != nil {
		fmt.Println("bad offset:", err)
		os.Exit(1)
	}
	b := make([]byte, 1)
	if n, err := f.ReadAt(b, offset); err != nil || n != 1 {
		fmt.Println("read failed:", n, err)
		os.Exit(1)
	}
	b[0] += 1
	if n, err := f.WriteAt(b, offset); err != nil || n != 1 {
		fmt.Println("read failed:", n, err)
		os.Exit(1)
	}
}
