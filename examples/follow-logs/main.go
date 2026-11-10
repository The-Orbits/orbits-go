// Command follow-logs prints a deployment's build log, following it until the build ends:
//
//	ORBITS_TOKEN=orb_live_… go run ./examples/follow-logs <team> <project> <deployment>
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	orbits "github.com/The-Orbits/orbits-go"
)

func main() {
	if len(os.Args) != 4 {
		log.Fatal("usage: follow-logs <team> <project> <dpl_…>")
	}
	c, err := orbits.NewFromEnv(orbits.WithUserAgent("follow-logs-example/1.0"))
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	for ev, err := range c.Deployments.StreamLogs(ctx, os.Args[1], os.Args[2], os.Args[3], nil) {
		if err != nil {
			log.Fatal(err)
		}
		switch ev.Type {
		case orbits.LogEventLine:
			fmt.Printf("%-11s %s\n", ev.Line.Step, ev.Line.Text)
		case orbits.LogEventDone:
			if f := ev.Done.Failure; f != nil {
				log.Fatalf("%s: %s", f.Title, f.Message)
			}
			fmt.Println("Build", ev.Done.State)
		}
	}
}
