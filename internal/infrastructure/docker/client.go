package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type Adapter struct {
	cli *client.Client
}

// NewAdapter inicializa el cliente de Docker.
func NewAdapter() (*Adapter, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("error inicializando docker client: %w", err)
	}
	return &Adapter{cli: cli}, nil
}

// GetStatus devuelve el estado actual de un contenedor por su ID o nombre.
func (a *Adapter) GetStatus(ctx context.Context, containerID string) (string, error) {
	info, err := a.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("error inspeccionando contenedor: %w", err)
	}
	return info.State.Status, nil // Ej: "running", "exited", "created"
}

// Start arranca un contenedor detenido.
func (a *Adapter) Start(ctx context.Context, containerID string) error {
	opts := container.StartOptions{}
	if err := a.cli.ContainerStart(ctx, containerID, opts); err != nil {
		return fmt.Errorf("error arrancando contenedor: %w", err)
	}
	return nil
}

// Stop detiene un contenedor en ejecución (con timeout predeterminado).
func (a *Adapter) Stop(ctx context.Context, containerID string) error {
	opts := container.StopOptions{}
	if err := a.cli.ContainerStop(ctx, containerID, opts); err != nil {
		return fmt.Errorf("error deteniendo contenedor: %w", err)
	}
	return nil
}

// Restart reinicia el contenedor.
func (a *Adapter) Restart(ctx context.Context, containerID string) error {
	opts := container.StopOptions{}
	if err := a.cli.ContainerRestart(ctx, containerID, opts); err != nil {
		return fmt.Errorf("error reiniciando contenedor: %w", err)
	}
	return nil
}

// Close cierra la conexión del cliente de Docker.
func (a *Adapter) Remove(ctx context.Context, containerID string) error {
	return a.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force: true,
	})
}

func (a *Adapter) Close() error {
	return a.cli.Close()
}
