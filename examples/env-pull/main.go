// Command env-pull writes a project's development variables to .env.local, as `orbits env pull` does:
//
//	ORBITS_TOKEN=orb_live_… go run ./examples/env-pull <team> <project>
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	orbits "github.com/The-Orbits/orbits-go"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: env-pull <team> <project>")
	}
	c, err := orbits.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	pull, err := c.Env.Pull(context.Background(), os.Args[1], os.Args[2], orbits.EnvironmentNameDevelopment)
	if err != nil {
		log.Fatal(err)
	}
	keys := make([]string, 0, len(pull.Variables))
	for k := range pull.Variables {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, strconv.Quote(pull.Variables[k]))
	}
	if err := os.WriteFile(".env.local", []byte(b.String()), 0o600); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Wrote %d variables to .env.local\n", len(keys))
	if len(pull.SkippedSensitive) > 0 {
		fmt.Printf("Left out %d sensitive ones, which can't be read back: %s\n", len(pull.SkippedSensitive), strings.Join(pull.SkippedSensitive, ", "))
	}
}
