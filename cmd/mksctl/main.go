// Command mksctl 是 provision 佈建腳本庫的 Go 版 CLI。
package main

import (
	"os"

	"github.com/flysangel/mksctl/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
