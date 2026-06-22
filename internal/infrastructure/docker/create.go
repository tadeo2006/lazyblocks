package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/tadeo2006/lazyblocks/internal/config"
)

// CreateAndStart creates the container if it doesn't exist, and starts it.
func (a *Adapter) CreateAndStart(ctx context.Context, inst config.Instance, cb func(string)) error {
	// Verificar si ya existe
	_, err := a.cli.ContainerInspect(ctx, inst.ContainerName)
	if err == nil {
		if cb != nil {
			cb("Container already exists, starting directly...")
		}
		return a.Start(ctx, inst.ContainerName)
	}

	if cb != nil {
		cb("La imagen 'itzg/minecraft-server' no está verificada o necesita actualizarse. Descargando (esto puede tardar unos minutos)...")
	}

	reader, err := a.cli.ImagePull(ctx, "docker.io/itzg/minecraft-server:latest", image.PullOptions{})
	if err != nil {
		return fmt.Errorf("error downloading image: %w", err)
	}
	// Consumir el stream para esperar a que termine
	io.Copy(io.Discard, reader)
	reader.Close()

	if cb != nil {
		cb("Image ready. Configuring ports, volumes, and environment variables...")
	}

	rconPort := nat.Port("25575/tcp")
	mcPort := nat.Port("25565/tcp")

	portMap := nat.PortMap{}
	
	// Configuramos RCON mapping
	if inst.RCON.Enabled {
		portMap[rconPort] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", inst.RCON.Port)},
		}
	}
	
	// Asumimos puerto de MC derivado del puerto RCON o fijo
	// En un escenario real, la configuración tendría un puerto de MC. Por ahora mapeamos el estándar.
	// Si inst.RCON.Port es 25575, el MC es 25565. Si RCON es 25576, MC es 25566.
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
		cb(fmt.Sprintf("Container '%s' created successfully. Starting...", resp.ID[:12]))
	}

	return a.Start(ctx, resp.ID)
}
