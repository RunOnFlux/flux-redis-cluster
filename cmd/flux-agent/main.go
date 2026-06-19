package main

import (
	"fmt"
	"log"
	"os"
)

var version = "dev"

func usage() {
	fmt.Fprintf(os.Stderr, `flux-agent — Flux Redis cluster agent (version %s)

Usage:
  flux-agent <subcommand> [args]

Subcommands:
  init         Run one-shot cluster initialization
  daemon       Run the cluster reconciliation loop
  proxy        Run TCP proxy routing writes to the current Redis master
  version      Print version and exit
  help         Print this help message
`, agentVersion())
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "init":
		logAgentVersion("init")
		runInit(os.Args[2:])
	case "daemon":
		logAgentVersion("daemon")
		runDaemon(os.Args[2:])
	case "proxy":
		logAgentVersion("proxy")
		runProxy(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println(agentVersion())
	case "help", "--help", "-h":
		usage()
	default:
		log.Printf("unknown subcommand: %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}
