package main

import (
	"os"

	"github.com/greprules/greprules/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.Execute(os.Args[1:], version))
}
