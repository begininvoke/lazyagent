#!/usr/bin/env bash
# Assemble Lazyagent.app from a built binary.
#
# Usage: scripts/make-app.sh BINARY VERSION OUTDIR
#   BINARY  — path to the full lazyagent binary (universal or single-arch)
#   VERSION — version string for the Info.plist (e.g. 0.4.0 or "dev")
#   OUTDIR  — directory to create Lazyagent.app in
#
# Signs the bundle via scripts/codesign.sh when APPLE_SIGNING_IDENTITY is
# set; otherwise leaves it unsigned (local/dev use).

set -euo pipefail

BINARY="$1"
VERSION="$2"
OUTDIR="$3"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

APP="$OUTDIR/Lazyagent.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

# --- Icon: rasterize assets/appicon.svg into an .icns -------------------
ICONSET="$(mktemp -d)/Lazyagent.iconset"
mkdir -p "$ICONSET"
SVG="$REPO_ROOT/assets/appicon.svg"

render() { # render SIZE OUTPNG
  local size="$1" out="$2"
  if command -v rsvg-convert >/dev/null 2>&1; then
    rsvg-convert -w "$size" -h "$size" "$SVG" -o "$out"
  else
    # qlmanage names its output <basename>.svg.png in the target dir.
    local tmp
    tmp="$(mktemp -d)"
    qlmanage -t -s "$size" -o "$tmp" "$SVG" >/dev/null
    mv "$tmp"/*.png "$out"
    rm -rf "$tmp"
  fi
}

BASE_PNG="$(mktemp -d)/appicon_1024.png"
render 1024 "$BASE_PNG"
for size in 16 32 64 128 256 512; do
  sips -z "$size" "$size" "$BASE_PNG" --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
  double=$((size * 2))
  sips -z "$double" "$double" "$BASE_PNG" --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/Lazyagent.icns"

# --- Info.plist ---------------------------------------------------------
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key><string>com.illegalstudio.lazyagent</string>
	<key>CFBundleName</key><string>Lazyagent</string>
	<key>CFBundleDisplayName</key><string>Lazyagent</string>
	<key>CFBundleExecutable</key><string>lazyagent</string>
	<key>CFBundleIconFile</key><string>Lazyagent</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>${VERSION}</string>
	<key>CFBundleVersion</key><string>${VERSION}</string>
	<key>LSMinimumSystemVersion</key><string>11.0</string>
	<key>LSUIElement</key><true/>
	<key>NSHighResolutionCapable</key><true/>
	<key>NSPrincipalClass</key><string>NSApplication</string>
</dict>
</plist>
PLIST

# --- Binary + signature -------------------------------------------------
cp "$BINARY" "$APP/Contents/MacOS/lazyagent"
chmod 0755 "$APP/Contents/MacOS/lazyagent"
plutil -lint "$APP/Contents/Info.plist"
"$SCRIPT_DIR/codesign.sh" "$APP"
echo "Assembled $APP"
