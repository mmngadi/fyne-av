#!/usr/bin/env bash
# fetch-ffmpeg.sh — shallow-clone FFmpeg at a pinned stable tag.
set -euo pipefail

TAG="${FFMPEG_TAG:-n7.1}"
SRC_DIR="${FFMPEG_SRC:-$(cd "$(dirname "$0")" && pwd)/ffmpeg}"

if [ -d "$SRC_DIR/.git" ]; then
	echo "[fetch] FFmpeg already present at $SRC_DIR"
	exit 0
fi

echo "[fetch] Cloning FFmpeg tag=$TAG -> $SRC_DIR"
rm -rf "$SRC_DIR"
git clone --depth 1 --branch "$TAG" https://github.com/FFmpeg/FFmpeg.git "$SRC_DIR"
echo "[fetch] done."