package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/supporttools/dr-syncer/pkg/agent/daemon"
	"github.com/supporttools/dr-syncer/pkg/agent/ssh"
)

var (
	sshPort = flag.Int("ssh-port", 2222, "SSH server port")
)

func main() {
	flag.Parse()

	// Initialize SSH server
	sshServer, err := ssh.NewServer(*sshPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize SSH server: %v\n", err)
		os.Exit(1)
	}

	// Initialize daemon
	d := daemon.NewDaemon(sshServer)

	// Start the daemon
	if err := d.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start daemon: %v\n", err)
		os.Exit(1)
	}

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// Graceful shutdown
	if err := d.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "Error during shutdown: %v\n", err)
		os.Exit(1)
	}
}
