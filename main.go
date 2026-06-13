package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"scrinium/cmd/scrinium"
)

func main() {
	log.SetOutput(os.Stderr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Check if directory exists
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: scrinium <scrinium.json>")
		os.Exit(1)
	}

	configPath := os.Args[1]

	app, err := scrinium.NewApp(configPath)
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
