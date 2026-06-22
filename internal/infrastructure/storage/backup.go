package storage

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupWorld zips the world directory inside dataDir and creates a .tar.gz backup
func BackupWorld(dataDir string, progressCb func(string)) (string, error) {
	worldDir := filepath.Join(dataDir, "world")
	if _, err := os.Stat(worldDir); os.IsNotExist(err) {
		return "", fmt.Errorf("world does not exist at %s", worldDir)
	}

	backupsDir := filepath.Join(dataDir, "backups")
	os.MkdirAll(backupsDir, os.ModePerm)

	timestamp := time.Now().Format("20060102_150405")
	outName := filepath.Join(backupsDir, fmt.Sprintf("world_backup_%s.tar.gz", timestamp))

	out, err := os.Create(outName)
	if err != nil {
		return "", fmt.Errorf("error creating file: %v", err)
	}
	defer out.Close()

	gw := gzip.NewWriter(out)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	err = filepath.Walk(worldDir, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}

		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return err
		}
		
		// Rel path inside the tar
		relPath := strings.TrimPrefix(strings.Replace(file, worldDir, "", -1), string(filepath.Separator))
		if relPath == "" {
			return nil
		}
		header.Name = filepath.Join("world", relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()

		if progressCb != nil {
			progressCb(fmt.Sprintf("Comprimiendo: %s", relPath))
		}

		_, err = io.Copy(tw, f)
		return err
	})

	if err != nil {
		os.Remove(outName) // Clean up on failure
		return "", fmt.Errorf("backup error: %v", err)
	}

	return outName, nil
}

// ListBackups returns a list of available backup file names
func ListBackups(dataDir string) ([]string, error) {
	backupsDir := filepath.Join(dataDir, "backups")
	var backups []string
	
	files, err := os.ReadDir(backupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return backups, nil
		}
		return nil, err
	}

	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".tar.gz") {
			backups = append(backups, f.Name())
		}
	}
	return backups, nil
}

// RestoreWorld extrae un backup reemplazando el mundo actual
func RestoreWorld(dataDir string, backupName string, progressCb func(string)) error {
	backupPath := filepath.Join(dataDir, "backups", backupName)
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup does not exist: %s", backupPath)
	}

	worldDir := filepath.Join(dataDir, "world")
	// Limpiar mundo actual
	if progressCb != nil {
		progressCb("Eliminando mundo actual...")
	}
	os.RemoveAll(worldDir)

	f, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dataDir, header.Name)
		
		if progressCb != nil {
			progressCb(fmt.Sprintf("Extrayendo: %s", header.Name))
		}

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, os.ModePerm)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), os.ModePerm)
			outFile, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}
