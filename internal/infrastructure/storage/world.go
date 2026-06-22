package storage

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DeleteWorld deletes the current world directory completely.
func DeleteWorld(dataDir string) error {
	worldDir := filepath.Join(dataDir, "world")
	err := os.RemoveAll(worldDir)
	if err != nil {
		return fmt.Errorf("error deleting world: %w", err)
	}
	return nil
}

// CreateEmptyWorld clears the current world and creates an empty directory.
func CreateEmptyWorld(dataDir string) error {
	if err := DeleteWorld(dataDir); err != nil {
		return err
	}
	worldDir := filepath.Join(dataDir, "world")
	return os.MkdirAll(worldDir, os.ModePerm)
}

// ImportWorld extracts an archive (zip or tar.gz) into the world folder.
func ImportWorld(dataDir string, archivePath string, progressCb func(string)) error {
	if err := CreateEmptyWorld(dataDir); err != nil {
		return err
	}

	worldDir := filepath.Join(dataDir, "world")
	
	if strings.HasSuffix(archivePath, ".zip") {
		return extractZip(archivePath, worldDir, progressCb)
	} else if strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz") {
		return extractTarGz(archivePath, worldDir, progressCb)
	}
	return fmt.Errorf("unsupported format, only .zip or .tar.gz allowed")
}

func extractZip(src string, dest string, progressCb func(string)) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			continue // Prevent ZipSlip
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		if progressCb != nil {
			progressCb(fmt.Sprintf("Extrayendo: %s", f.Name))
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(src string, dest string, progressCb func(string)) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Handle potential root folder wrapping from backups (e.g., "world/level.dat")
		name := header.Name
		if strings.HasPrefix(name, "world/") {
			name = strings.TrimPrefix(name, "world/")
		}

		target := filepath.Join(dest, name)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), os.ModePerm); err != nil {
				return err
			}
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			
			if progressCb != nil {
				progressCb(fmt.Sprintf("Extrayendo: %s", name))
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
