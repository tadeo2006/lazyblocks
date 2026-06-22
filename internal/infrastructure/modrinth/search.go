package modrinth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type SearchResult struct {
	Hits []struct {
		ProjectID   string `json:"project_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		ProjectType string `json:"project_type"`
	} `json:"hits"`
}

type VersionResult []struct {
	Files []struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
	} `json:"files"`
}

// Search busca proyectos en Modrinth (mods, plugins, etc.)
func Search(query string, limit int) (*SearchResult, error) {
	url := fmt.Sprintf("https://api.modrinth.com/v2/search?query=%s&limit=%d", query, limit)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("modrinth API devolvió: %s", resp.Status)
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// DownloadLatest descarga la última versión de un proyecto a una carpeta destino
func DownloadLatest(projectID, projectType, dataDir string, cb func(string)) error {
	// Obtener versiones
	url := fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version", projectID)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var versions VersionResult
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return err
	}

	if len(versions) == 0 || len(versions[0].Files) == 0 {
		return fmt.Errorf("no se encontraron archivos descargables para este proyecto")
	}

	targetFile := versions[0].Files[0]

	// Decidir carpeta destino
	targetDir := filepath.Join(dataDir, "mods")
	if projectType == "plugin" {
		targetDir = filepath.Join(dataDir, "plugins")
	} else if projectType == "datapack" {
		targetDir = filepath.Join(dataDir, "world", "datapacks")
	}

	if err := os.MkdirAll(targetDir, os.ModePerm); err != nil {
		return err
	}

	destPath := filepath.Join(targetDir, targetFile.Filename)
	if cb != nil {
		cb(fmt.Sprintf("Descargando %s...", targetFile.Filename))
	}

	// Descargar
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	dlResp, err := http.Get(targetFile.URL)
	if err != nil {
		return err
	}
	defer dlResp.Body.Close()

	if _, err := io.Copy(out, dlResp.Body); err != nil {
		return err
	}

	if cb != nil {
		cb(fmt.Sprintf("✅ Instalado exitosamente en /%s", filepath.Base(targetDir)))
	}

	return nil
}
