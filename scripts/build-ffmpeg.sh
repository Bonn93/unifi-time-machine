#!/bin/sh

# Builds FFmpeg from source with the encoders the app uses:
#   libsvtav1 (preferred AV1), libvpx-vp9 (fallback), libx264 (HLS/MP4).
# Used by the Dockerfile's ffmpeg stage and by the ffmpeg check in
# .chainguard/ci.yaml, so both build the same binary.
#
# Usage: build-ffmpeg.sh PREFIX
# Requires (Wolfi): build-base nasm pkgconf curl xz zlib-dev x264-dev svt-av1-dev libvpx-dev

set -eu

FFMPEG_VERSION="9.0.2"
FFMPEG_SHA256="8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e"

PREFIX="${1:?usage: build-ffmpeg.sh PREFIX}"
SRC="$(mktemp -d)"

curl -fsSL -o "$SRC/ffmpeg.tar.xz" "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz"
echo "${FFMPEG_SHA256}  $SRC/ffmpeg.tar.xz" | sha256sum -c -
tar -xJf "$SRC/ffmpeg.tar.xz" -C "$SRC" --strip-components=1

cd "$SRC"
# FFmpeg's own libraries are linked statically; the codec libraries stay
# shared so the runtime image installs them with apk.
./configure \
    --prefix="$PREFIX" \
    --disable-debug \
    --disable-doc \
    --disable-ffplay \
    --disable-shared \
    --enable-static \
    --enable-gpl \
    --enable-libsvtav1 \
    --enable-libvpx \
    --enable-libx264
make -j"$(nproc)"
make install

# Record the shared libraries the binaries need as apk "so:" names, for the
# runtime stage to install.
readelf -d "$PREFIX/bin/ffmpeg" "$PREFIX/bin/ffprobe" \
    | sed -n 's/.*Shared library: \[\(.*\)\]/so:\1/p' \
    | sort -u > "$PREFIX/runtime-deps"

cd /
rm -rf "$SRC"
