#!/usr/bin/env bash
# build-ffmpeg-linux.sh — build static FFmpeg archives for linux/amd64.
# Output: libs/linux_x86_64/{lib,include}
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="${FFMPEG_SRC:-$ROOT/build/ffmpeg}"
OUT="$ROOT/libs/linux_x86_64"
JOBS="${JOBS:-$(nproc 2>/dev/null || echo 4)}"

"$ROOT/build/fetch-ffmpeg.sh"

mkdir -p "$OUT"
cd "$SRC"

# Common curated decoder/demuxer set (see PRD).
DECODERS="h264,hevc,vp8,vp9,av1,mpeg4,mpeg2video,theora,aac,aac_fixed,mp3,mp3float,mp3fixed,flac,opus,vorbis,pcm_s16le,pcm_s24le,pcm_f32le,alac"
DEMUXERS="mov,matroska,webm,avi,flv,mp3,ogg,wav,aac,mpegvideo,mpegts"
PARSERS="h264,hevc,vp8,vp9,opus,aac,mpeg4video,mpegaudio,vc1"

./configure \
	--prefix="$OUT" \
	--enable-gpl \
	--enable-static \
	--disable-shared \
	--enable-pic \
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
	--extra-cflags="-fPIC -O2"

make -j"$JOBS"
make install
make distclean

echo "[linux] Built static archives -> $OUT/lib"
ls -la "$OUT/lib"