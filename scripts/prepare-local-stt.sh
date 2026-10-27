#!/usr/bin/env bash
# Prepares the DEMO-mode local transcriber for the portable Windows package:
#   build/dist/whisper/  whisper-cli.exe + DLLs (official whisper.cpp CPU build)
#   build/dist/models/   ggml-whisper-ro.bin (Romanian model, q5_0) + silero VAD
#
# Model choice (MODEL=…):
#   ro    (default) eek/whisper-turbo-ro-en: Whisper large-v3-turbo fine-tuned for
#         spoken Romanian + English, MIT. Gated on Hugging Face: accept its terms
#         with your HF account once, then export HF_TOKEN=<read token> (never commit it).
#   stock openai/whisper-large-v3-turbo: ungated, for testing the pipeline.
#
# Optional: VCRUNTIME_DIR=<folder with msvcp140.dll, vcruntime140.dll,
# vcruntime140_1.dll> copies the Visual C++ runtime next to whisper-cli.exe
# (app-local deployment) for laptops that don't have it installed.
#
# Converting needs Python 3 and builds whisper.cpp's quantizer with cmake/clang
# (into build/cache). Everything is pinned and checksum-verified.
set -euo pipefail
cd "$(dirname "$0")/.."

MODEL="${MODEL:-ro}"
CACHE=build/cache
DIST=build/dist
mkdir -p "$CACHE" "$DIST/whisper" "$DIST/models"

WHISPER_TAG=v1.9.5
WHISPER_COMMIT=d1be6fde11ac6e0407606b4e42fe72d34add8037
WHISPER_ZIP_URL=https://github.com/ggml-org/whisper.cpp/releases/download/b5454/whisper-bin-x64.zip
WHISPER_ZIP_SHA=6ba69e3482d7826214f90a6a9c84ca07782aec1e1d0c6a7c30c994fd5d816ccb
VAD_URL=https://huggingface.co/ggml-org/whisper-vad/resolve/main/ggml-silero-v6.2.0.bin
VAD_SHA=2aa269b785eeb53a82983a20501ddf7c1d9c48e33ab63a41391ac6c9f7fb6987
MEL_URL=https://raw.githubusercontent.com/openai/whisper/c5d42560760a05584c1c79546a098287e5a771eb/whisper/assets/mel_filters.npz
MEL_SHA=7450ae70723a5ef9d341e3cee628c7cb0177f36ce42c44b7ed2bf3325f0f6d4c

fetch() { # url sha out
  if [ ! -f "$3" ] || ! echo "$2  $3" | shasum -a 256 -c - >/dev/null 2>&1; then
    curl -fsSL --retry 3 -C - -o "$3" "$1" || curl -fsSL --retry 3 -o "$3" "$1"
  fi
  echo "$2  $3" | shasum -a 256 -c -
}

# 1. whisper-cli for Windows (CPU only: picks the best ggml-cpu-*.dll at runtime).
fetch "$WHISPER_ZIP_URL" "$WHISPER_ZIP_SHA" "$CACHE/whisper-bin-x64.zip"
rm -rf "$CACHE/whisper-unzip" && mkdir -p "$CACHE/whisper-unzip"
unzip -q "$CACHE/whisper-bin-x64.zip" 'Release/whisper-cli.exe' 'Release/whisper.dll' 'Release/ggml*.dll' -d "$CACHE/whisper-unzip"
cp "$CACHE"/whisper-unzip/Release/* "$DIST/whisper/"
if [ -n "${VCRUNTIME_DIR:-}" ]; then
  for f in msvcp140.dll vcruntime140.dll vcruntime140_1.dll; do cp "$VCRUNTIME_DIR/$f" "$DIST/whisper/"; done
fi

# 2. Voice activity detection model (skips silence; faster, no invented text).
fetch "$VAD_URL" "$VAD_SHA" "$DIST/models/ggml-silero-v6.2.0.bin"

# 3. Romanian model → ggml → q5_0.
OUT="$DIST/models/ggml-whisper-ro.bin"
if [ -f "$OUT" ] && [ -z "${FORCE:-}" ]; then
  echo "model already prepared: $OUT (FORCE=1 to rebuild)"
  exit 0
fi
case "$MODEL" in
  ro)    REPO=eek/whisper-turbo-ro-en;     REV=30c9c8863948f9c3525d71975d72fa71c9dd94d7
         [ -n "${HF_TOKEN:-}" ] || { echo "HF_TOKEN is not set (see the header of this script)" >&2; exit 1; } ;;
  stock) REPO=openai/whisper-large-v3-turbo; REV=41f01f3fe87f28c78e2fbf8b568835947dd65ed9 ;;
  *)     echo "MODEL must be ro or stock" >&2; exit 1 ;;
esac
SRC="$CACHE/hf-$MODEL"
mkdir -p "$SRC"
hf_get() { # file
  local url="https://huggingface.co/$REPO/resolve/$REV/$1" part="$SRC/$1.part"
  if [ -n "${HF_TOKEN:-}" ]; then
    # The token is passed as a header through a file descriptor: never on the
    # command line (process list) and never written to disk.
    curl -fsSL --retry 3 -C - -o "$part" -H @<(printf 'Authorization: Bearer %s\n' "$HF_TOKEN") "$url"
  else
    curl -fsSL --retry 3 -C - -o "$part" "$url"
  fi
  mv "$part" "$SRC/$1"
}
for f in config.json vocab.json added_tokens.json model.safetensors; do
  [ -s "$SRC/$f" ] || hf_get "$f"
done
fetch "$MEL_URL" "$MEL_SHA" "$CACHE/mel_filters.npz"

python3 -m venv "$CACHE/venv"
"$CACHE/venv/bin/pip" -q install numpy safetensors cmake
"$CACHE/venv/bin/python" -I scripts/convert-whisper-to-ggml.py "$SRC" "$CACHE/mel_filters.npz" "$CACHE/ggml-$MODEL-f16.bin"

if [ ! -x "$CACHE/whisper.cpp/build/bin/whisper-quantize" ]; then
  rm -rf "$CACHE/whisper.cpp"
  git clone -q --depth 1 -b "$WHISPER_TAG" https://github.com/ggml-org/whisper.cpp "$CACHE/whisper.cpp"
  [ "$(git -C "$CACHE/whisper.cpp" rev-parse HEAD)" = "$WHISPER_COMMIT" ] || { echo "whisper.cpp tag moved" >&2; exit 1; }
  "$CACHE/venv/bin/cmake" -S "$CACHE/whisper.cpp" -B "$CACHE/whisper.cpp/build" -DCMAKE_BUILD_TYPE=Release -DGGML_METAL=OFF -DGGML_BLAS=OFF >/dev/null
  "$CACHE/venv/bin/cmake" --build "$CACHE/whisper.cpp/build" -j 8 --target whisper-quantize >/dev/null
fi
DYLD_LIBRARY_PATH="$CACHE/whisper.cpp/build/bin" LD_LIBRARY_PATH="$CACHE/whisper.cpp/build/bin" \
  "$CACHE/whisper.cpp/build/bin/whisper-quantize" "$CACHE/ggml-$MODEL-f16.bin" "$OUT" q5_0 >/dev/null
shasum -a 256 "$OUT"
echo "prepared $OUT ($MODEL)"
