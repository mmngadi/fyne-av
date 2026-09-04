#!/usr/bin/env bash
# build-all.sh — fetch FFmpeg and build static archives for all supported targets,
# then tar each into dist/ for upload to GitHub Releases.
#
# Targets:
#   linux/amd64        (host gcc)
#   android/arm64      (NDK)
#   android/amd64      (NDK, emulator)
#   windows/amd64      (MinGW-w64 cross-compiler)
#
# macOS/iOS are not built here — see BUILD.md "macOS / iOS (help wanted)".
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

FFMPEG_TAG_VAL="${FFMPEG_TAG:-n7.1}"
DIST="$ROOT/dist"
mkdir -p "$DIST"

echo "==== Building FFmpeg tag=$FFMPEG_TAG_VAL for linux/amd64 ===="
bash build/build-ffmpeg-linux.sh

echo
echo "==== Building FFmpeg for android/arm64 ===="
bash build/build-ffmpeg-android.sh

echo
echo "==== Building FFmpeg for android/amd64 ===="
bash build/build-ffmpeg-android-amd64.sh

echo
echo "==== Building FFmpeg for windows/amd64 ===="
bash build/build-ffmpeg-windows.sh

echo
echo "==== Packaging static archives ===="
tar_targets=(
	"linux_x86_64:libs-linux-x86_64"
	"android_arm64:libs-android-arm64"
	"android_amd64:libs-android-amd64"
	"windows_x86_64:libs-windows-x86_64"
)
for pair in "${tar_targets[@]}"; do
	dir="${pair%%:*}"
	name="${pair##*:}"
	if [ -d "$ROOT/libs/$dir/lib" ]; then
		tar -czf "$DIST/${name}.tar.gz" -C "$ROOT/libs" "$dir"
		echo "  -> dist/${name}.tar.gz ($(du -h "$DIST/${name}.tar.gz" | cut -f1))"
	else
		echo "  [skip] libs/$dir not built"
	fi
done

echo
echo "==== Done. Upload dist/*.tar.gz to the GitHub Release. ===="
ls -la "$DIST"