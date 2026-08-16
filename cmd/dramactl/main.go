// Command dramactl 是微短剧内容审核与分账结算平台的命令行入口。
package main

import (
	"os"

	"microdrama/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
