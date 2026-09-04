// Command fetch-libs downloads the prebuilt FFmpeg static archives for a
// target platform from the fyne-av GitHub Releases and extracts them into
// the fyne-av module's libs/ directory (inside the Go module cache) so that
// cgo's ${SRCDIR} references resolve at build time.
//
// Install:
//
//	go install github.com/mmngadi/fyne-av/cmd/fetch-libs@latest
//
// Usage:
//
//	fetch-libs                           # host platform
//	fetch-libs -os android -arch arm64   # physical device
//	fetch-libs -os android -arch amd64   # emulator
//	fetch-libs -os windows -arch amd64   # cross-compile from Linux
//
// Override the release tag with FYNE_AV_LIBS_TAG (default: v0.3.0) and the
// base URL with FYNE_AV_LIBS_BASE (default:
// https://github.com/mmngadi/fyne-av/releases/download).
package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// platformDir maps (GOOS, GOARCH) to the directory name used inside the
// tarball and under libs/.
func platformDir(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return "linux_x86_64", nil
	case "android/arm64":
		return "android_arm64", nil
	case "android/amd64":
		return "android_amd64", nil
	case "windows/amd64":
		return "windows_x86_64", nil
	default:
		return "", fmt.Errorf("fetch-libs: no prebuilt libraries for %s/%s; "+
			"see BUILD.md to build FFmpeg from source", goos, goarch)
	}
}

// assetName maps a platform directory to the release asset filename.
func assetName(dir string) string {
	switch dir {
	case "linux_x86_64":
		return "libs-linux-x86_64.tar.gz"
	case "android_arm64":
		return "libs-android-arm64.tar.gz"
	case "android_amd64":
		return "libs-android-amd64.tar.gz"
	case "windows_x86_64":
		return "libs-windows-x86_64.tar.gz"
	default:
		return ""
	}
}

// moduleDir returns the on-disk directory of the fyne-av module in the
// local Go module cache by invoking `go list -m`.
func moduleDir() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/mmngadi/fyne-av").Output()
	if err != nil {
		return "", fmt.Errorf("go list failed: %w (is github.com/mmngadi/fyne-av in your go.mod?)", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" || dir == "github.com/mmngadi/fyne-av" {
		return "", fmt.Errorf("could not resolve fyne-av module dir; ensure it is in go.mod and run `go mod download` first")
	}
	return dir, nil
}

func main() {
	targetOS := flag.String("os", "", "target OS (linux, android, windows); defaults to host")
	targetArch := flag.String("arch", "", "target arch (amd64, arm64); defaults to host")
	flag.Parse()

	tag := os.Getenv("FYNE_AV_LIBS_TAG")
	if tag == "" {
		tag = "v0.3.2"
	}
	base := os.Getenv("FYNE_AV_LIBS_BASE")
	if base == "" {
		base = "https://github.com/mmngadi/fyne-av/releases/download"
	}

	goos := *targetOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := *targetArch
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	dir, err := platformDir(goos, goarch)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	modDir, err := moduleDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch-libs:", err)
		os.Exit(1)
	}
	libsRoot := filepath.Join(modDir, "libs")
	target := filepath.Join(libsRoot, dir)

	asset := assetName(dir)
	url := fmt.Sprintf("%s/%s/%s", base, tag, asset)

	fmt.Printf("fetch-libs: %s/%s -> %s\n", goos, goarch, url)
	fmt.Printf("fetch-libs: target = %s\n", target)

	if _, err := os.Stat(filepath.Join(target, "lib")); err == nil {
		fmt.Printf("fetch-libs: libs/%s already present, skipping (delete it to re-fetch)\n", dir)
		return
	}

	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch-libs: download failed:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "fetch-libs: download failed: HTTP %s\n", resp.Status)
		os.Exit(1)
	}

	// The module cache is typically read-only; make libs/ writable.
	if err := os.MkdirAll(libsRoot, 0o755); err != nil {
		_ = exec.Command("chmod", "-R", "u+w", modDir).Run()
		if err2 := os.MkdirAll(libsRoot, 0o755); err2 != nil {
			fmt.Fprintln(os.Stderr, "fetch-libs: mkdir libs failed:", err2)
			os.Exit(1)
		}
	}
	_ = exec.Command("chmod", "-R", "u+w", libsRoot).Run()

	if err := extractTarGz(resp.Body, libsRoot); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-libs: extract failed:", err)
		os.Exit(1)
	}

	fmt.Printf("fetch-libs: extracted to %s\n", target)
}

func extractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		path := filepath.Join(dest, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			os.Remove(path)
			if err := os.Symlink(hdr.Linkname, path); err != nil {
				return err
			}
		}
	}
	return nil
}