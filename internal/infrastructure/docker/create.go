package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/tadeo2006/lazyblocks/internal/config"
)

// pullEvent is a JSON event line from the Docker image pull stream
type pullEvent struct {
	Status   string `json:"status"`
	Progress string `json:"progress"`
	ID       string `json:"id"`
	Error    string `json:"error"`
}

// CreateAndStart creates the container if it doesn't exist, and starts it.
func (a *Adapter) CreateAndStart(ctx context.Context, inst config.Instance, cb func(string)) error {
	// Check if container already exists
	_, err := a.cli.ContainerInspect(ctx, inst.ContainerName)
	if err == nil {
		if cb != nil {
			cb("Container already exists, starting directly...")
		}
		return a.Start(ctx, inst.ContainerName)
	}

	if cb != nil {
		cb("Pulling Docker image 'itzg/minecraft-server:latest'...")
		cb("This may take a few minutes on the first run.")
	}

	reader, err := a.cli.ImagePull(ctx, "docker.io/itzg/minecraft-server:latest", image.PullOptions{})
	if err != nil {
		return fmt.Errorf("error downloading image: %w", err)
	}
	defer reader.Close()

	// Parse the JSON progress stream and forward progress to the callback
	scanner := bufio.NewScanner(reader)
	layerStatus := map[string]string{} // track per-layer status

	for scanner.Scan() {
		var evt pullEvent
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}
		if evt.Error != "" {
			return fmt.Errorf("docker pull error: %s", evt.Error)
		}
		if cb == nil {
			continue
		}

		if evt.ID != "" && evt.Status != "" {
			// Deduplicate: only emit when the status changes for a layer
			key := evt.ID
			label := evt.Status
			if evt.Progress != "" {
				label = evt.Status + " " + evt.Progress
			}
			if layerStatus[key] != label {
				layerStatus[key] = label
				// Filter noisy "Waiting" / "Already exists" lines
				if !strings.HasPrefix(evt.Status, "Waiting") {
					cb(fmt.Sprintf("[PULL] Layer %s: %s", evt.ID, label))
				}
			}
		} else if evt.Status != "" {
			cb(fmt.Sprintf("[PULL] %s", evt.Status))
		}
	}

	if cb != nil {
		cb("Image ready. Configuring ports, volumes, and environment variables...")
	}

	rconPort := nat.Port("25575/tcp")
	mcPort := nat.Port("25565/tcp")

	portMap := nat.PortMap{}

	// Map RCON port
	if inst.RCON.Enabled {
		portMap[rconPort] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", inst.RCON.Port)},
		}
	}

	// Derive Minecraft port from RCON port (RCON=25575 -> MC=25565, etc.)
	mcHostPort := inst.RCON.Port - 10
	portMap[mcPort] = []nat.PortBinding{
		{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", mcHostPort)},
	}

	env := []string{
		"EULA=TRUE",
		"TYPE=" + strings.ToUpper(inst.Type),
		"ENABLE_RCON=true",
		"RCON_PORT=25575",
	}

	if inst.MCVersion != "" && inst.MCVersion != "latest" {
		env = append(env, "VERSION="+inst.MCVersion)
	}

	password := os.Getenv(inst.RCON.PasswordEnv)
	if password == "" {
		password = "secret-dev-password"
	}
	env = append(env, "RCON_PASSWORD="+password)

	if inst.Memory != "" {
		env = append(env, "MEMORY="+inst.Memory)
	}

	os.MkdirAll(inst.Paths.DataDir, os.ModePerm)

	resp, err := a.cli.ContainerCreate(ctx, &container.Config{
		Image: "itzg/minecraft-server:latest",
		Env:   env,
		ExposedPorts: nat.PortSet{
			rconPort: struct{}{},
			mcPort:   struct{}{},
		},
	}, &container.HostConfig{
		PortBindings: portMap,
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: inst.Paths.DataDir,
				Target: "/data",
			},
		},
	}, &network.NetworkingConfig{}, nil, inst.ContainerName)
	if err != nil {
		return fmt.Errorf("error creating container: %w", err)
	}

	if cb != nil {
		cb(fmt.Sprintf("Container '%s' created. Starting server...", resp.ID[:12]))
	}

	return a.Start(ctx, resp.ID)
}
