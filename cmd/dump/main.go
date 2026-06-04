package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/functionfly/ff-cli/internal/flypy"
	"github.com/functionfly/ff-cli/internal/flypy/backend"
	"github.com/functionfly/ff-cli/internal/flypy/ir"
	"github.com/functionfly/ff-cli/internal/flypy/parser"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: dump <mode> <output-file>")
		os.Exit(1)
	}
	mode := os.Args[1]
	outFile := os.Args[2]

	source := os.Args[3]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ast, err := parser.ParsePython(ctx, source)
	if err != nil {
		fmt.Println("parse err:", err)
		os.Exit(1)
	}
	module, err := ir.Generate(ast, "test")
	if err != nil {
		fmt.Println("ir err:", err)
		os.Exit(1)
	}
	var m flypy.ExecutionMode
	if mode == "complex" {
		m = flypy.ComplexMode
	} else if mode == "compatible" {
		m = flypy.CompatibleMode
	} else {
		m = flypy.DeterministicMode
	}
	rust, err := backend.GenerateRustWithMode(module, string(m))
	if err != nil {
		fmt.Println("rust err:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outFile, []byte(rust), 0600); err != nil {
		fmt.Println("write err:", err)
		os.Exit(1)
	}
	fmt.Println("Dumped to", outFile)
}
