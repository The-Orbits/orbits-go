// Command deploy-folder deploys a folder to an upload project and follows its build log:
//
//	ORBITS_TOKEN=orb_live_… go run ./examples/deploy-folder <team> <project> <folder>
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	orbits "github.com/The-Orbits/orbits-go"
)

func main() {
	if len(os.Args) != 4 {
		log.Fatal("usage: deploy-folder <team> <project> <folder>")
	}
	team, project, dir := os.Args[1], os.Args[2], os.Args[3]

	c, err := orbits.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	d, err := c.Deployments.UploadDir(ctx, team, project, dir, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Building", d.ID)

	for ev, err := range c.Deployments.StreamLogs(ctx, team, project, d.ID, nil) {
		if err != nil {
			log.Fatal(err)
		}
		switch ev.Type {
		case orbits.LogEventLine:
			fmt.Println(ev.Line.Text)
		case orbits.LogEventDone:
			fmt.Println("Build", ev.Done.State)
		}
	}

	final, err := c.Deployments.Wait(ctx, team, project, d.ID, nil)
	var failed *orbits.DeploymentError
	switch {
	case errors.As(err, &failed):
		log.Fatal(err)
	case err != nil:
		log.Fatal(err)
	}
	if final.URL != nil {
		fmt.Println("Live at", *final.URL)
	}
}
