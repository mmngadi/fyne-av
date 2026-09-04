#!/usr/bin/env bash
# build-ffmpeg-android-amd64.sh — build static FFmpeg archives for android x86_64.
# Output: libs/android_amd64/{lib,include}
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="${FFMPEG_SRC:-$ROOT/build/ffmpeg}"
OUT="$ROOT/libs/android_amd64"
JOBS="${JOBS:-$(nproc 2>/dev/null || echo 4)}"

NDK="${ANDROID_NDK_HOME:-$HOME/android/ndk/android-ndk-r27d}"
API=24
HOST="x86_64-linux-android"
TOOLCHAIN="$NDK/toolchains/llvm/prebuilt/linux-x86_64"
SYSROOT="$TOOLCHAIN/sysroot"
CC="$TOOLCHAIN/bin/${HOST}${API}-clang"
CXX="$TOOLCHAIN/bin/${HOST}${API}-clang++"
AR="$TOOLCHAIN/bin/llvm-ar"
RANLIB="$TOOLCHAIN/bin/llvm-ranlib"
STRIP="$TOOLCHAIN/bin/llvm-strip"

if [ ! -x "$CC" ]; then
	echo "[android-amd64] NDK compiler not found: $CC" >&2
	exit 1
fi

"$ROOT/build/fetch-ffmpeg.sh"

mkdir -p "$OUT"
cd "$SRC"

DECODERS="h264,hevc,vp8,vp9,av1,mpeg4,mpeg2video,theora,aac,aac_fixed,mp3,mp3float,mp3fixed,flac,opus,vorbis,pcm_s16le,pcm_s24le,pcm_f32le,alac"
DEMUXERS="mov,matroska,webm,avi,flv,mp3,ogg,wav,aac,mpegvideo,mpegts"
PARSERS="h264,hevc,vp8,vp9,opus,aac,mpeg4video,mpegaudio,vc1"

./configure \
	--prefix="$OUT" \
	--enable-gpl \
	--enable-static \
	--disable-shared \
	--enable-pic \
	--enable-small \
	--enable-cross-compile \
	--disable-programs \
	--disable-doc \
	--disable-network \
	--disable-asm \
	--disable-everything \
	--enable-decoder=$DECODERS \
	--enable-demuxer=$DEMUXERS \
	--enable-parser=$PARSERS \
	--enable-protocol=file \
	--disable-zlib \
	--enable-swscale \
	--enable-swresample \
	--target-os=android \
	--arch=x86_64 \
	--cc="$CC" \
	--cxx="$CXX" \
	--ar="$AR" \
	--ranlib="$RANLIB" \
	--strip="$STRIP" \
	--sysroot="$SYSROOT" \
	--extra-cflags="-fPIC -O2 -ffunction-sections -fstack-protector-strong" \
	--extra-ldflags="-Wl,--gc-sections"

make -j"$JOBS"
make install
make distclean

echo "[android-amd64] Built static archives -> $OUT/lib"
ls -la "$OUT/lib"