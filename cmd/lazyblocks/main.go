package main

import (
	"context"
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
	// Si el usuario pasa un comando CLI explícito, usamos la lógica anterior
	if len(os.Args) > 1 {
		runCLI(os.Args[1])
		return
	}

	// Por defecto, arrancamos la TUI
	cfg, err := config.LoadConfig("configs/local.example.yaml")
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}


	dockerAdapter, err := docker.NewAdapter()
	if err != nil {
		log.Fatalf("Error initializing Docker adapter: %v", err)
	}
	defer dockerAdapter.Close()

	// Iniciar gocui App
	app, err := ui.NewApp(cfg, dockerAdapter)
	if err != nil {
		log.Fatalf("Error initializing UI: %v", err)
	}
	
	if err := app.Run(); err != nil && err != gocui.ErrQuit {
		log.Fatalf("Error running TUI: %v", err)
	}
}

// runCLI mantiene la lógica de pruebas por comandos que hicimos en las Fases 1 y 2
func runCLI(command string) {
	cfg, err := config.LoadConfig("configs/local.example.yaml")
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}
	if len(cfg.Instances) == 0 {
		log.Fatalf("No instances defined for CLI")
	}
	instance := cfg.Instances[0]

	if command == "rcon" {
		rconCmd := strings.Join(os.Args[2:], " ")
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
		if len(os.Args) < 3 {
			log.Fatalf("Usage: lazyblocks crond <instance-id>")
		}
		instanceID := os.Args[2]
		
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
		// TODO: Implementar limitación de backups (targetInstance.Backup.Keep)
		fmt.Println("=> Backup completed successfully")
	}
}
