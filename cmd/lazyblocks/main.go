package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jesseduffield/gocui"
	"github.com/tadeo2006/lazyblocks/internal/config"
	"github.com/tadeo2006/lazyblocks/internal/infrastructure/docker"
	"github.com/tadeo2006/lazyblocks/internal/infrastructure/rcon"
	"github.com/tadeo2006/lazyblocks/internal/infrastructure/storage"
	"github.com/tadeo2006/lazyblocks/internal/ui"
)

func main() {
	configPathFlag := flag.String("config", "", "Explicit path to configuration file")
	forceFlag := flag.Bool("force", false, "Force overwrite (used with init)")
	flag.Parse()

	args := flag.Args()
	if len(args) > 0 {
		command := args[0]
		switch command {
		case "init":
			path, err := config.InitConfig(*forceFlag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error initializing config: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Configuration created at:\n%s\n\nEdit this file and then run:\nlazyblocks\n", path)
			return
		case "config":
			if len(args) > 1 && args[1] == "path" {
				resolvedPath, err := config.ResolveConfigPath(*configPathFlag)
				if err != nil {
					if errors.Is(err, config.ErrConfigNotFound) {
						fmt.Println("No active configuration found.")
						os.Exit(1)
					}
					fmt.Fprintf(os.Stderr, "Error resolving config path: %v\n", err)
					os.Exit(1)
				}
				fmt.Println(resolvedPath)
				return
			}
		}
		
		cfg, _ := loadConfigOrDie(*configPathFlag)
		runCLI(args[0], cfg)
		return
	}

	// Default TUI mode
	cfg, resolvedPath := loadConfigOrDie(*configPathFlag)

	dockerAdapter, err := docker.NewAdapter()
	if err != nil {
		log.Fatalf("Error initializing Docker adapter: %v", err)
	}
	defer dockerAdapter.Close()

	app, err := ui.NewApp(cfg, resolvedPath, dockerAdapter)
	if err != nil {
		log.Fatalf("Error initializing UI: %v", err)
	}
	
	if err := app.Run(); err != nil && err != gocui.ErrQuit {
		log.Fatalf("Error running TUI: %v", err)
	}
}

func loadConfigOrDie(flagPath string) (*config.Config, string) {
	cfg, resolvedPath, err := config.Load(flagPath)
	if err != nil {
		if errors.Is(err, config.ErrConfigNotFound) {
			fmt.Fprintln(os.Stderr, "No LazyBlocks configuration was found.\n\nCreate one with:\n  lazyblocks init\n\nOr provide one explicitly:\n  lazyblocks --config /path/to/config.yaml")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}
	return cfg, resolvedPath
}

func runCLI(command string, cfg *config.Config) {
	if len(cfg.Instances) == 0 {
		log.Fatalf("No instances defined for CLI")
	}
	instance := cfg.Instances[0]

	if command == "rcon" {
		rconCmd := strings.Join(flag.Args()[1:], " ")
		password := os.Getenv(instance.RCON.PasswordEnv)
		if password == "" {
			password = "secret-dev-password"
		}
		client, err := rcon.Dial(instance.RCON.Host, instance.RCON.Port, password)
		if err != nil {
			log.Fatalf("RCON Error: %v", err)
		}
		defer client.Close()

		output, err := client.Execute(rconCmd)
		if err != nil {
			log.Fatalf("Error executing: %v", err)
		}
		fmt.Printf("=> Response:\n%s\n", output)
		return
	}

	dockerAdapter, err := docker.NewAdapter()
	if err != nil {
		log.Fatalf("Docker Error: %v", err)
	}
	defer dockerAdapter.Close()

	ctx := context.Background()
	switch command {
	case "status":
		status, _ := dockerAdapter.GetStatus(ctx, instance.ContainerName)
		fmt.Printf("=> Container '%s' status: %s\n", instance.ContainerName, status)
	case "start":
		dockerAdapter.Start(ctx, instance.ContainerName)
		fmt.Println("=> Started")
	case "stop":
		dockerAdapter.Stop(ctx, instance.ContainerName)
		fmt.Println("=> Stopped")
	case "restart":
		dockerAdapter.Restart(ctx, instance.ContainerName)
		fmt.Println("=> Restarted")
	case "logs":
		reader, _ := dockerAdapter.StreamLogs(ctx, instance.ContainerName, "10")
		defer reader.Close()
		buf := make([]byte, 1024)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				fmt.Print(string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
	case "crond":
		if len(flag.Args()) < 2 {
			log.Fatalf("Usage: lazyblocks crond <instance-id>")
		}
		instanceID := flag.Args()[1]
		
		var targetInstance *config.Instance
		for _, inst := range cfg.Instances {
			if inst.ID == instanceID {
				targetInstance = &inst
				break
			}
		}
		if targetInstance == nil {
			log.Fatalf("Instance not found: %s", instanceID)
		}
		
		fmt.Printf("=> Running background backup for: %s\n", targetInstance.Name)
		_, err := storage.BackupWorld(targetInstance.Paths.DataDir, func(msg string) {})
		if err != nil {
			log.Fatalf("crond backup error: %v", err)
		}
		fmt.Println("=> Backup completed successfully")
	}
}
