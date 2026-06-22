package modrinth

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type IndexJSON struct {
	Dependencies map[string]string `json:"dependencies"`
	Files        []struct {
		Path      string `json:"path"`
		Downloads []string `json:"downloads"`
		Env       *struct {
			Server string `json:"server"`
		} `json:"env"`
	} `json:"files"`
}

type MrPackInfo struct {
	Type          string
	MCVersion     string
	LoaderVersion string
}

// InstallMrPack extracts overrides and concurrently downloads mods from a .mrpack
func InstallMrPack(mrpackPath string, destDir string, progressCb func(string, int, int)) (*MrPackInfo, error) {
	r, err := zip.OpenReader(mrpackPath)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir el mrpack: %w", err)
	}
	defer r.Close()

	var index *IndexJSON
	for _, f := range r.File {
		if f.Name == "modrinth.index.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			err = json.NewDecoder(rc).Decode(&index)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("error parseando modrinth.index.json: %w", err)
			}
		} else if strings.HasPrefix(f.Name, "overrides/") || strings.HasPrefix(f.Name, "server-overrides/") {
			// Extraer overrides
			prefix := "overrides/"
			if strings.HasPrefix(f.Name, "server-overrides/") {
				prefix = "server-overrides/"
			}
			relPath := strings.TrimPrefix(f.Name, prefix)
			if relPath == "" {
				continue
			}

			targetPath := filepath.Join(destDir, relPath)
			if f.FileInfo().IsDir() {
				os.MkdirAll(targetPath, os.ModePerm)
				continue
			}

			os.MkdirAll(filepath.Dir(targetPath), os.ModePerm)
			outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				continue
			}

			rc, err := f.Open()
			if err == nil {
				io.Copy(outFile, rc)
				rc.Close()
			}
			outFile.Close()
		}
	}

	if index == nil {
		return nil, fmt.Errorf("no se encontró modrinth.index.json")
	}

	info := &MrPackInfo{
		MCVersion: index.Dependencies["minecraft"],
		Type:      "PAPER", // fallback
	}

	if loaderVer, ok := index.Dependencies["fabric-loader"]; ok {
		info.Type = "FABRIC"
		info.LoaderVersion = loaderVer
	} else if loaderVer, ok := index.Dependencies["forge"]; ok {
		info.Type = "FORGE"
		info.LoaderVersion = loaderVer
	} else if loaderVer, ok := index.Dependencies["quilt-loader"]; ok {
		info.Type = "QUILT"
		info.LoaderVersion = loaderVer
	}

	// Filter valid files (ignore client-only)
	var toDownload []string
	var toPaths []string
	for _, file := range index.Files {
		if file.Env != nil && file.Env.Server == "unsupported" {
			continue // Client only mod
		}
		if len(file.Downloads) > 0 {
			toDownload = append(toDownload, file.Downloads[0])
			toPaths = append(toPaths, filepath.Join(destDir, file.Path))
		}
	}

	total := len(toDownload)
	if total == 0 {
		return info, nil
	}

	var wg sync.WaitGroup
	var completed int
	var mu sync.Mutex
	errChan := make(chan error, total)
	semaphore := make(chan struct{}, 15) // Max 15 concurrent downloads

	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(url string, path string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := downloadFile(url, path); err != nil {
				errChan <- err
				return
			}

			mu.Lock()
			completed++
			c := completed
			mu.Unlock()

			if progressCb != nil {
				progressCb(filepath.Base(path), c, total)
			}
		}(toDownload[i], toPaths[i])
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			return nil, fmt.Errorf("error durante las descargas: %w", err)
		}
	}

	return info, nil
}

func downloadFile(url string, dest string) error {
	os.MkdirAll(filepath.Dir(dest), os.ModePerm)
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	_, err = io.Copy(out, resp.Body)
	return err
}
