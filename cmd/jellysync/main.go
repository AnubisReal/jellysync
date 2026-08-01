package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AnubisReal/jellysync/internal/app"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := app.Run(logger); err != nil {
		logger.Error("jellysync stopped", "error", err)
		os.Exit(1)
	}
}

func healthcheck() error {
	addr := os.Getenv("JELLYSYNC_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + addr + "/health")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck returned %s", response.Status)
	}
	return nil
}
