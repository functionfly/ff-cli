// Package main implements the FunctionFly developer CLI.
package main

import (
	"github.com/functionfly/ff-cli/cmd/fly/commands"
)

func main() {
	root := commands.NewRootCmd()
	if err := root.Execute(); err != nil {
		commands.ExitOnError(err)
	}
}
