# Building fyne-av

## Prerequisites

### Linux (host build + cross-compile for Android)

```bash
# Fedora / RHEL
sudo dnf install -y nasm alsa-lib-devel pkgconf-pkg-config

# Debian / Ubuntu
sudo apt install -y nasm libasound2-dev pkg-config
```

Required tools:
- Go 1.21+
- `git`
- `make`
- `pkg-config`
- `nasm` (for FFmpeg x86 SIMD optimizations)
- `gcc` (host C compiler)

### Android

- Android NDK (tested with r27d)
- Set `ANDROID_NDK_HOME`:
  ```bash
  export ANDROID_NDK_HOME=$HOME/android/ndk/android-ndk-r27d
  ```

### Windows (cross-compile from Linux)

Windows builds use the MinGW-w64 cross-compiler:
```bash
# Fedora / RHEL
sudo dnf install -y mingw64-gcc nasm

# Debian / Ubuntu
sudo apt install -y mingw-w64 nasm
```

The build script auto-detects `x86_64-w64-mingw32-gcc`. If your toolchain lives elsewhere, override via env vars:

```bash
MINGW_CC=/opt/mingw64/bin/x86_64-w64-mingw32-gcc \
MINGW_PREFIX=/opt/mingw64/x86_64-w64-mingw32 \
bash build/build-ffmpeg-windows.sh
```

### macOS / iOS (help wanted)

Needs FFmpeg static libraries compiled on a macOS host. The general approach:

1. **macOS (universal)** — build FFmpeg twice (arm64 and x86_64) with the Xcode toolchain, then combine with `lipo` into fat archives under `libs/macos_universal/{lib,include}`. A `build-ffmpeg-macos.sh` script is wanted.

2. **iOS (arm64)** — build FFmpeg against the iOS SDK and package the `.a` archives (or an `.xcframework`) under `libs/ios_arm64/`. Apple does not allow dynamically loaded code in iOS apps, so static linking is the only option. A `build-ffmpeg-ios.sh` script is wanted.

Key configure flags for Apple platforms: `--enable-cross-compile --target-os=darwin --arch=arm64 --cc=clang --extra-cflags="-fembed-bitcode -isysroot ..."`.

If you have a Mac and can contribute these scripts and verify playback, please open a PR — see the "Help Wanted" note in the README.

## Building FFmpeg Static Archives

From the repo root:

```bash
# Build all supported targets (linux + android arm64 + android amd64 + windows amd64)
# and tar each into dist/ for GitHub Releases upload.
bash build/build-all.sh

# Or individually:
bash build/build-ffmpeg-linux.sh
bash build/build-ffmpeg-android.sh          # arm64-v8a (physical devices)
bash build/build-ffmpeg-android-amd64.sh    # x86_64 (emulator testing)
bash build/build-ffmpeg-windows.sh          # windows/amd64 (MinGW cross-compile)
```

This fetches FFmpeg `n7.1` and builds static `.a` archives into:
- `libs/linux_x86_64/{lib,include}`
- `libs/android_arm64/{lib,include}`
- `libs/android_amd64/{lib,include}`
- `libs/windows_x86_64/{lib,include}`

`build-all.sh` also produces `dist/libs-*.tar.gz` archives ready to attach to a GitHub Release.

### FFmpeg Version

Default tag: `n7.1`. Override:
```bash
FFMPEG_TAG=n7.0 bash build/build-all.sh
```

### Customizing Decoders

Edit the `DECODERS`, `DEMUXERS`, and `PARSERS` variables in the build scripts under `build/`.

## Building a Fyne App with fyne-av

### Linux Desktop

```bash
CGO_ENABLED=1 go build .
```

### Android (emulator / x86_64 device)

```bash
fyne package -os android/amd64 -appID com.example.myapp
```

### Android (physical arm64 device)

```bash
fyne package -os android/arm64 -appID com.example.myapp
```

Install:
```bash
adb install myapp.apk
```

### Windows

Cross-compile from Linux with MinGW-w64. You must point `CC`/`CXX` at the MinGW gcc — Go's default `gcc` won't understand Windows cgo flags like `-mthreads`:

```bash
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
  CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
  go build -o app.exe .
```

For a fully static `.exe` (no runtime DLL dependency on `libwinpthread`), add `CGO_LDFLAGS=-static`:

```bash
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
  CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
  CGO_LDFLAGS=-static go build -o app.exe .
```

## Producing GitHub Release Assets

After running `build-all.sh`, upload the tarballs in `dist/` to a new GitHub Release whose tag matches the `fyne-av` version (e.g. `v0.1.0`). The fetch-libs command downloads from that release by default. To override the version/URL:

```bash
FYNE_AV_LIBS_TAG=v0.1.1 go run github.com/mmngadi/fyne-av/cmd/fetch-libs
```

## License Notes

- `fyne-av` Go code: MIT
- FFmpeg static archives: GPL (built with `--enable-gpl`)