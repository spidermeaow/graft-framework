package main

import (
	"github.com/spidermeaow/graft-framework"
)

func main() {
	app := graft.New()
	app.Use(graft.RequestID(), graft.Logger(), graft.Recovery())
	app.GET("/", func(c *graft.Context) error { return c.JSON(200, map[string]string{"message": "Hello Graft"}) })
	graft.PrintStartup("8080")
	if err := app.Run(":8080"); err != nil {
		graft.Fatal(err)
	}
}
