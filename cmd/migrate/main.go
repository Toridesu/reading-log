package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"readinglog/internal/database"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "status") {
		return errors.New("usage: migrate up|status")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	provider, err := database.Migrator(db)
	if err != nil {
		return err
	}
	switch os.Args[1] {
	case "up":
		results, err := provider.Up(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("applied %d migration(s)\n", len(results))
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return err
		}
		for _, s := range statuses {
			fmt.Printf("%05d %s %s\n", s.Source.Version, s.State, s.Source.Path)
		}
	}
	return nil
}
