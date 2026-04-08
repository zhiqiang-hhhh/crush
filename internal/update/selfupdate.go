package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SelfUpdate downloads the latest release binary and replaces the
// current executable. Returns the new version string on success.
func SelfUpdate(ctx context.Context) (string, error) {
	release, err := Default.Latest(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest release: %w", err)
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH

	var assetName string
	if goos == "windows" {
		assetName = fmt.Sprintf("crush-%s-%s.zip", goos, goarch)
	} else {
		assetName = fmt.Sprintf("crush-%s-%s.tar.gz", goos, goarch)
	}

	var downloadURL string
	tag := release.TagName
	downloadURL = fmt.Sprintf("https://github.com/amosbird/crush/releases/download/%s/%s", tag, assetName)

	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to find current executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("failed to resolve executable path: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Extract binary to a temp file in the same directory as the current
	// executable (so rename is atomic on the same filesystem).
	dir := filepath.Dir(exe)
	tmpFile, err := os.CreateTemp(dir, "crush-update-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	var binaryName string
	if goos == "windows" {
		binaryName = "crush.exe"
	} else {
		binaryName = "crush"
	}

	if strings.HasSuffix(assetName, ".zip") {
		if err := extractFromZip(resp.Body, binaryName, tmpFile); err != nil {
			tmpFile.Close()
			return "", err
		}
	} else {
		if err := extractFromTarGz(resp.Body, binaryName, tmpFile); err != nil {
			tmpFile.Close()
			return "", err
		}
	}
	tmpFile.Close()

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return "", fmt.Errorf("failed to set executable permission: %w", err)
	}

	// On Windows, rename the running exe first (Windows allows renaming
	// but not overwriting a running executable).
	if goos == "windows" {
		oldPath := exe + ".old"
		os.Remove(oldPath)
		if err := os.Rename(exe, oldPath); err != nil {
			return "", fmt.Errorf("failed to rename old executable: %w", err)
		}
	}

	if err := os.Rename(tmpPath, exe); err != nil {
		return "", fmt.Errorf("failed to replace executable: %w", err)
	}

	return strings.TrimPrefix(tag, "v"), nil
}

func extractFromTarGz(r io.Reader, name string, w io.Writer) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("binary %q not found in archive", name)
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		if filepath.Base(hdr.Name) == name {
			if _, err := io.Copy(w, tr); err != nil {
				return fmt.Errorf("extract: %w", err)
			}
			return nil
		}
	}
}

func extractFromZip(r io.Reader, name string, w *os.File) error {
	// zip needs random access, so buffer to a temp file first.
	tmp, err := os.CreateTemp("", "crush-zip-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	size, err := io.Copy(tmp, r)
	if err != nil {
		return fmt.Errorf("buffering zip: %w", err)
	}

	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		return fmt.Errorf("zip: %w", err)
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) == name {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			if _, err := io.Copy(w, rc); err != nil {
				return fmt.Errorf("extract: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("binary %q not found in zip", name)
}
