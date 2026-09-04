#!/usr/bin/env bash
# build-ffmpeg-windows.sh — cross-compile static FFmpeg archives for windows/amd64.
# Output: libs/windows_x86_64/{lib,include}
# Requires MinGW-w64 cross-compiler (mingw64-gcc on Fedora, mingw-w64 on Debian/Ubuntu).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="${FFMPEG_SRC:-$ROOT/build/ffmpeg}"
OUT="$ROOT/libs/windows_x86_64"
JOBS="${JOBS:-$(nproc 2>/dev/null || echo 4)}"

# MinGW-w64 toolchain (Fedora prefixes tools with x86_64-w64-mingw32-).
HOST="x86_64-w64-mingw32"
PREFIX="${MINGW_PREFIX:-/usr/x86_64-w64-mingw32/sys-root/mingw}"
CC="${MINGW_CC:-${HOST}-gcc}"
CXX="${MINGW_CXX:-${HOST}-g++}"
AR="${MINGW_AR:-${HOST}-gcc-ar}"
RANLIB="${MINGW_RANLIB:-${HOST}-gcc-ranlib}"
STRIP="${MINGW_STRIP:-${HOST}-strip}"
PKG_CONFIG="${MINGW_PKG_CONFIG:-${HOST}-pkg-config}"

if ! command -v "$CC" >/dev/null 2>&1; then
	echo "[windows] MinGW compiler not found: $CC" >&2
	echo "[windows] Install it first:" >&2
	echo "    Fedora:    sudo dnf install mingw64-gcc nasm" >&2
	echo "    Debian:    sudo apt install mingw-w64 nasm" >&2
	exit 1
fi

"$ROOT/build/fetch-ffmpeg.sh"

mkdir -p "$OUT"
cd "$SRC"

# Same curated decoder/demuxer set as the other platforms.
DECODERS="h264,hevc,vp8,vp9,av1,mpeg4,mpeg2video,theora,aac,aac_fixed,mp3,mp3float,mp3fixed,flac,opus,vorbis,pcm_s16le,pcm_s24le,pcm_f32le,alac"
DEMUXERS="mov,matroska,webm,avi,flv,mp3,ogg,wav,aac,mpegvideo,mpegts"
PARSERS="h264,hevc,vp8,vp9,opus,aac,mpeg4video,mpegaudio,vc1"

# Notes:
#   --target-os=mingw32   — FFmpeg's name for the Windows/MinGW target.
#   --enable-pic          — needed so the archives can be linked into cgo.
#   nasm is available and safe on x86_64; FFmpeg auto-detects it.
#   No -lz/-lbz2: we --disable-zlib and don't enable bzip2/lzma.
#   winpthreads ships with MinGW-w64; FFmpeg finds it via -pthread.
./configure \
	--prefix="$OUT" \
	--enable-gpl \
	--enable-static \
	--disable-shared \
	--enable-pic \
	--enable-cross-compile \
	--disable-programs \
	--disable-doc \
	--disable-network \
	--disable-everything \
	--enable-decoder=$DECODERS \
	--enable-demuxer=$DEMUXERS \
	--enable-parser=$PARSERS \
	--enable-protocol=file \
	--disable-zlib \
	--disable-bzlib \
	--disable-iconv \
	--disable-sdl2 \
	--enable-swscale \
	--enable-swresample \
	--target-os=mingw32 \
	--arch=x86_64 \
	--cc="$CC" \
	--cxx="$CXX" \
	--ar="$AR" \
	--ranlib="$RANLIB" \
	--strip="$STRIP" \
	--pkg-config="$PKG_CONFIG" \
	--extra-cflags="-fPIC -O2 -I${PREFIX}/include" \
	--extra-ldflags="-L${PREFIX}/lib" \
	--extra-libs="-lwinpthread -lm -lws2_32 -lbcrypt -lsecur32"

make -j"$JOBS"
make install
make distclean

echo "[windows] Built static archives -> $OUT/lib"
ls -la "$OUT/lib"