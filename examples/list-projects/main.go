// Command list-projects lists a team's projects and their latest deployment:
//
//	ORBITS_TOKEN=orb_live_… go run ./examples/list-projects <team>
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	orbits "github.com/The-Orbits/orbits-go"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: list-projects <team>")
	}
	c, err := orbits.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	for p, err := range c.Projects.All(context.Background(), os.Args[1], nil) {
		if err != nil {
			log.Fatal(err)
		}
		latest := "-"
		if p.LatestDeployment != nil {
			latest = string(p.LatestDeployment.State)
		}
		fmt.Printf("%-30s %-10s %s\n", p.Slug, p.Status, latest)
	}
}
