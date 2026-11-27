#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

TARGETS="${1:-darwin/amd64,darwin/arm64}"
echo "==> Building Velan.app ($TARGETS) via mygo..."
go tool mygo build -platform "$TARGETS" -skip-dmg .

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

IFS=',' read -ra TARGET_LIST <<< "$TARGETS"
for TARGET in "${TARGET_LIST[@]}"; do
  PLAT="${TARGET//\//-}"
  APP="build/${PLAT}/Velan.app"
  BIN="${APP}/Contents/MacOS/Velan"
  PLAYER_DIR="${APP}/Contents/Resources/player"

  if [[ ! -x "$BIN" ]]; then
    echo "error: built binary not found at $BIN" >&2
    exit 1
  fi

  if [[ -n "$SRC_MPV" ]]; then
    echo "==> [$PLAT] Bundling libmpv from $SRC_MPV..."
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
                link_dst = os.path.join(dst_dir, name)
                if not os.path.exists(link_dst):
                    shutil.copy2(cand, link_dst)

print(f"Bundled {len(os.listdir(dst_dir))} dylibs into {dst_dir}")
PY
  fi

  echo "==> [$PLAT] Adding LC_RPATH to $BIN..."
  install_name_tool -add_rpath "@executable_path/../Resources/player" "$BIN" 2>/dev/null || true
  install_name_tool -add_rpath "/Applications/IINA.app/Contents/Frameworks" "$BIN" 2>/dev/null || true

  if [[ -f "assets/Velan.icns" ]]; then
    echo "==> [$PLAT] Bundling Velan.icns..."
    mkdir -p "${APP}/Contents/Resources"
    cp "assets/Velan.icns" "${APP}/Contents/Resources/AppIcon.icns"
    /usr/libexec/PlistBuddy -c "Delete :CFBundleIconFile" "${APP}/Contents/Info.plist" 2>/dev/null || true
    /usr/libexec/PlistBuddy -c "Delete :CFBundleIconName" "${APP}/Contents/Info.plist" 2>/dev/null || true
    /usr/libexec/PlistBuddy -c "Add :CFBundleIconFile string AppIcon.icns" "${APP}/Contents/Info.plist"
  fi

  echo "==> [$PLAT] Re-signing $APP..."
  codesign --force --deep --sign - "$APP"
  touch "$APP" "${APP}/Contents/Info.plist"
  /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$APP" 2>/dev/null || true
  echo "==> Done: $APP"
done
