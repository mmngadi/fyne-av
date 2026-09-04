#!/usr/bin/env bash
# build-ffmpeg-android.sh — build static FFmpeg archives for android arm64-v8a.
# Output: libs/android_arm64/{lib,include}
# Requires Android NDK at $ANDROID_NDK_HOME (default: ~/android/ndk/android-ndk-r27d).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="${FFMPEG_SRC:-$ROOT/build/ffmpeg}"
OUT="$ROOT/libs/android_arm64"
JOBS="${JOBS:-$(nproc 2>/dev/null || echo 4)}"

NDK="${ANDROID_NDK_HOME:-$HOME/android/ndk/android-ndk-r27d}"
API=24
HOST="aarch64-linux-android"
TOOLCHAIN="$NDK/toolchains/llvm/prebuilt/linux-x86_64"
SYSROOT="$TOOLCHAIN/sysroot"
CC="$TOOLCHAIN/bin/${HOST}${API}-clang"
CXX="$TOOLCHAIN/bin/${HOST}${API}-clang++"
AR="$TOOLCHAIN/bin/llvm-ar"
RANLIB="$TOOLCHAIN/bin/llvm-ranlib"
STRIP="$TOOLCHAIN/bin/llvm-strip"

if [ ! -x "$CC" ]; then
	echo "[android] NDK compiler not found: $CC" >&2
	echo "[android] Set ANDROID_NDK_HOME to your NDK root." >&2
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
	--disable-everything \
	--enable-decoder=$DECODERS \
	--enable-demuxer=$DEMUXERS \
	--enable-parser=$PARSERS \
	--enable-protocol=file \
	--enable-swscale \
	--enable-swresample \
	--disable-asm \
	--target-os=android \
	--arch=aarch64 \
	--cc="$CC" \
	--cxx="$CXX" \
	--ar="$AR" \
	--ranlib="$RANLIB" \
	--strip="$STRIP" \
	--sysroot="$SYSROOT" \
	--extra-cflags="-fPIC -O2 -ffunction-sections -funwind-tables -fstack-protector-strong" \
	--extra-ldflags="-Wl,--gc-sections"

make -j"$JOBS"
make install
make distclean

echo "[android] Built static archives -> $OUT/lib"
ls -la "$OUT/lib"