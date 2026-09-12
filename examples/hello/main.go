package main

import (
	"github.com/spidermeaow/graft-framework"
	"log"
)

func main() {
	app := graft.New()
	app.Use(graft.RequestID(), graft.Logger(), graft.Recovery())
	app.GET("/", func(c *graft.Context) error { return c.JSON(200, map[string]string{"message": "Hello Graft"}) })
	if err := app.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
