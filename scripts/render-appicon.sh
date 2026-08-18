#!/usr/bin/env bash
# Regenerate assets/appicon.png (1024px, transparent background) from
# assets/appicon.svg. Run this whenever the SVG artwork changes.
#
# Uses NSImage via JXA: macOS 11+ renders SVG natively and preserves the
# alpha channel. Do NOT swap this for qlmanage — its thumbnailer flattens
# SVGs onto an opaque white background (that shipped once as a white
# border around the app icon).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SVG="$REPO_ROOT/assets/appicon.svg"
OUT="$REPO_ROOT/assets/appicon.png"

osascript -l JavaScript - "$SVG" "$OUT" <<'JXA'
ObjC.import("AppKit");
function run(argv) {
  const svg = $.NSImage.alloc.initWithContentsOfFile(argv[0]);
  if (svg.isNil()) throw "failed to load " + argv[0];
  const rep = $.NSBitmapImageRep.alloc
    .initWithBitmapDataPlanesPixelsWidePixelsHighBitsPerSampleSamplesPerPixelHasAlphaIsPlanarColorSpaceNameBytesPerRowBitsPerPixel(
      null, 1024, 1024, 8, 4, true, false, $.NSCalibratedRGBColorSpace, 0, 0);
  $.NSGraphicsContext.saveGraphicsState;
  $.NSGraphicsContext.setCurrentContext(
    $.NSGraphicsContext.graphicsContextWithBitmapImageRep(rep));
  // 2 = NSCompositingOperationSourceOver
  svg.drawInRectFromRectOperationFraction(
    $.NSMakeRect(0, 0, 1024, 1024), $.NSZeroRect, 2, 1.0);
  $.NSGraphicsContext.currentContext.flushGraphics;
  $.NSGraphicsContext.restoreGraphicsState;
  const corner = rep.colorAtXY(0, 0);
  if (corner.alphaComponent > 0.001) throw "corner not transparent — rendering regressed";
  // 4 = NSBitmapImageFileTypePNG
  const png = rep.representationUsingTypeProperties(4, $.NSDictionary.dictionary);
  png.writeToFileAtomically(argv[1], true);
  return "wrote " + argv[1] + " (corner alpha 0)";
}
JXA
