package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	server := flag.String("server", os.Getenv("GOMI_SERVER_URL"), "GOMI server URL (env: GOMI_SERVER_URL)")
	token := flag.String("token", os.Getenv("GOMI_AGENT_TOKEN"), "agent token (env: GOMI_AGENT_TOKEN)")
	interval := flag.Duration("interval", 5*time.Minute, "sync interval")
	imageDir := flag.String("image-dir", "/var/lib/libvirt/images", "image storage directory")
	flag.Parse()

	if *server == "" {
		log.Fatal("--server or GOMI_SERVER_URL is required")
	}
	if *token == "" {
		log.Fatal("--token or GOMI_AGENT_TOKEN is required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := Run(ctx, Config{
		ServerURL: *server,
		Token:     *token,
		Interval:  *interval,
		ImageDir:  *imageDir,
	}); err != nil {
		log.Fatalf("gomi-hypervisor: %v", err)
	}
}
