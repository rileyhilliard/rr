package main

import (
	"github.com/rileyhilliard/rr/internal/cli"
)

// Version info set via ldflags at build time:
//
//	go build -ldflags "-X main.version=1.0.0 -X main.commit=abc123 -X main.date=2024-01-01"
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cli.SetVersionInfo(version, commit, date)
	cli.Execute()
}
