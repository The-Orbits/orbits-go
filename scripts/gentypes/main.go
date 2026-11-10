// Command gentypes writes types.go and params.go from testdata/openapi.yaml. Run it from the repository root:
//
//	go run ./scripts/gentypes          # write the files
//	go run ./scripts/gentypes -check   # fail if they are out of date
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/The-Orbits/orbits-go/internal/gen"
	"github.com/The-Orbits/orbits-go/internal/schema"
)

func main() {
	check := flag.Bool("check", false, "report out-of-date files instead of writing them")
	specPath := flag.String("spec", "testdata/openapi.yaml", "the OpenAPI document")
	flag.Parse()

	spec, err := schema.Load(*specPath)
	if err != nil {
		fail(err)
	}
	types, params, err := gen.Generate(spec)
	if err != nil {
		fail(err)
	}
	stale := false
	for _, f := range []struct {
		path string
		data []byte
	}{{"types.go", types}, {"params.go", params}} {
		if *check {
			old, err := os.ReadFile(f.path)
			if err != nil || !bytes.Equal(old, f.data) {
				fmt.Fprintf(os.Stderr, "%s is out of date: run go run ./scripts/gentypes\n", f.path)
				stale = true
			}
			continue
		}
		if err := os.WriteFile(f.path, f.data, 0o644); err != nil {
			fail(err)
		}
	}
	if stale {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gentypes:", err)
	os.Exit(1)
}
