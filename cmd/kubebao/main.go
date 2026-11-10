package main

import (
	"context"
	"os"

	"github.com/kubebao/openbao-operator-cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
