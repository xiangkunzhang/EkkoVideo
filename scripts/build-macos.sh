#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> Building EkkoVideo.app via mygo..."
go tool mygo build -skip-dmg .

ARCH="$(uname -m)"
if [[ "$ARCH" == "x86_64" ]]; then
  PLAT="darwin-amd64"
else
  PLAT="darwin-arm64"
fi

APP="build/${PLAT}/EkkoVideo.app"
BIN="${APP}/Contents/MacOS/EkkoVideo"
PLAYER_DIR="${APP}/Contents/Resources/player"

if [[ ! -x "$BIN" ]]; then
  echo "error: built binary not found at $BIN" >&2
  exit 1
fi

# Locate source libmpv.2.dylib (or libmpv.dylib)
SRC_MPV=""
for cand in \
  "/Applications/IINA.app/Contents/Frameworks/libmpv.2.dylib" \
  "/opt/homebrew/opt/mpv/lib/libmpv.dylib" \
  "/usr/local/opt/mpv/lib/libmpv.dylib"; do
  if [[ -f "$cand" ]]; then
    SRC_MPV="$cand"
    break
  fi
done

if [[ -n "$SRC_MPV" ]]; then
  echo "==> Bundling libmpv from $SRC_MPV..."
  mkdir -p "$PLAYER_DIR"
  python3 - "$SRC_MPV" "$PLAYER_DIR" <<'PY'
import os, shutil, subprocess, sys
from collections import deque

src_mpv, dst_dir = sys.argv[1], sys.argv[2]
src_dir = os.path.dirname(os.path.realpath(src_mpv))

seen = set()
q = deque([os.path.realpath(src_mpv)])
while q:
    cur = q.popleft()
    if cur in seen or not os.path.exists(cur):
        continue
    seen.add(cur)
    dst = os.path.join(dst_dir, os.path.basename(cur))
    if not os.path.exists(dst):
        shutil.copy2(cur, dst)
    out = subprocess.check_output(["otool", "-L", cur], text=True)
    for line in out.splitlines()[1:]:
        dep = line.strip().split(" (", 1)[0]
        if dep.startswith("@rpath/"):
            name = dep[len("@rpath/"):]
            cand = os.path.join(src_dir, name)
            if os.path.exists(cand):
                q.append(os.path.realpath(cand))
                # Preserve symlink name if realpath differs
                link_dst = os.path.join(dst_dir, name)
                if not os.path.exists(link_dst):
                    shutil.copy2(cand, link_dst)

print(f"Bundled {len(os.listdir(dst_dir))} dylibs into {dst_dir}")
PY
fi

echo "==> Adding LC_RPATH to $BIN..."
install_name_tool -add_rpath "@executable_path/../Resources/player" "$BIN" 2>/dev/null || true
install_name_tool -add_rpath "/Applications/IINA.app/Contents/Frameworks" "$BIN" 2>/dev/null || true

echo "==> Re-signing $APP..."
codesign --force --deep --sign - "$APP"

echo "==> Done: $APP"
