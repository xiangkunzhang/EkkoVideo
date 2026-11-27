#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> Building Velan.exe (windows/amd64) via mygo..."
go tool mygo build -platform windows/amd64 -skip-installer .

OUT_DIR="build/windows-amd64"
EXE="${OUT_DIR}/Velan.exe"
if [[ ! -f "$EXE" ]]; then
  echo "error: built executable not found at $EXE" >&2
  exit 1
fi

# Bundle libmpv-2.dll if present locally (or via VELAN_WIN_LIBMPV)
WIN_DLL="${VELAN_WIN_LIBMPV:-}"
if [[ -z "$WIN_DLL" ]]; then
  for cand in "assets/windows/libmpv-2.dll" "assets/windows/mpv-2.dll"; do
    if [[ -f "$cand" ]]; then
      WIN_DLL="$cand"
      break
    fi
  done
fi

if [[ -n "$WIN_DLL" && -f "$WIN_DLL" ]]; then
  echo "==> Bundling $(basename "$WIN_DLL") into $OUT_DIR/resources/player..."
  mkdir -p "${OUT_DIR}/resources/player"
  cp "$WIN_DLL" "${OUT_DIR}/resources/player/libmpv-2.dll"
elif [[ -f "assets/windows/libmpv-2.dll.gz" ]]; then
  echo "==> Extracting assets/windows/libmpv-2.dll.gz into $OUT_DIR/resources/player..."
  mkdir -p "${OUT_DIR}/resources/player"
  gzip -dc "assets/windows/libmpv-2.dll.gz" > "${OUT_DIR}/resources/player/libmpv-2.dll"
else
  echo "warning: libmpv-2.dll(.gz) not found in assets/windows/; copy libmpv-2.dll into ${OUT_DIR}/resources/player/ before running on Windows." >&2
fi

echo "==> Packaging build/Velan-windows-amd64.zip..."
(cd build && rm -f Velan-windows-amd64.zip && zip -rq Velan-windows-amd64.zip windows-amd64)
echo "==> Done: $OUT_DIR (and build/Velan-windows-amd64.zip)"
