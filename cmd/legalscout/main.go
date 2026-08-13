package main

import (
	"fmt"
	"os"

	"github.com/Duan-JM/LegalScout/internal/cli"
)

func main() {
	root := cli.NewRoot(cli.DefaultDependencies())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}
