package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
)

// StreamLogs devuelve un lector que emite los logs en tiempo real.
// En Docker API, los logs vienen multiplexados (stdout/stderr).
// Se necesita stdcopy.StdCopy (del paquete docker) para separar o unificarlos en una app final,
// pero aquí devolveremos el io.ReadCloser base que debe ser decodificado.
func (a *Adapter) StreamLogs(ctx context.Context, containerID string, tail string) (io.ReadCloser, error) {
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       tail,
	}

	reader, err := a.cli.ContainerLogs(ctx, containerID, opts)
	if err != nil {
		return nil, fmt.Errorf("error obteniendo logs del contenedor: %w", err)
	}

	return reader, nil
}
