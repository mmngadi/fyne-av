// Command fetch-libs downloads the prebuilt FFmpeg static archives for the
// current target platform (GOOS/GOARCH) from the fyne-av GitHub Releases and
// extracts them into libs/ next to this module.
//
// It is invoked by `go generate` via the directive in generate.go:
//
//	go generate github.com/mmngadi/fyne-av
//
// Override the release tag with FYNE_AV_LIBS_TAG (default: v0.1.0) and the
// base URL with FYNE_AV_LIBS_BASE (default:
// https://github.com/mmngadi/fyne-av/releases/download).
package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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

func main() {
	tag := os.Getenv("FYNE_AV_LIBS_TAG")
	if tag == "" {
		tag = "v0.1.0"
	}
	base := os.Getenv("FYNE_AV_LIBS_BASE")
	if base == "" {
		base = "https://github.com/mmngadi/fyne-av/releases/download"
	}

	goos := os.Getenv("GOOS")
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := os.Getenv("GOARCH")
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	dir, err := platformDir(goos, goarch)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Find the module root (parent of cmd/fetch-libs).
	libsRoot, err := filepath.Abs(filepath.Join(filepath.Dir(os.Args[0]), "..", "..", "libs"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch-libs: cannot resolve libs/ path:", err)
		os.Exit(1)
	}
	// When run via `go run`, os.Args[0] is a temp dir; fall back to CWD.
	if _, err := os.Stat(filepath.Join(libsRoot, "..", "go.mod")); err != nil {
		cwd, _ := os.Getwd()
		libsRoot = filepath.Join(cwd, "libs")
	}

	target := filepath.Join(libsRoot, dir)
	asset := assetName(dir)
	url := fmt.Sprintf("%s/%s/%s", base, tag, asset)

	fmt.Printf("fetch-libs: %s/%s -> %s\n", goos, goarch, url)

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

	if err := os.MkdirAll(libsRoot, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-libs: mkdir libs failed:", err)
		os.Exit(1)
	}

	if err := extractTarGz(resp.Body, libsRoot); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-libs: extract failed:", err)
		os.Exit(1)
	}

	fmt.Printf("fetch-libs: extracted to libs/%s\n", dir)
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
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
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