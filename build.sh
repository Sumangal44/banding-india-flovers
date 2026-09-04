#!/usr/bin/env bash
# Build Banding India Flovers OS
# Custom Arch Linux distribution with Indian floral branding
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="$SCRIPT_DIR/build"
ISO_DIR="$BUILD_DIR/iso"
OUT_DIR="$BUILD_DIR/out"

echo "============================================="
echo "  Banding India Flovers OS Builder v1.0.0"
echo "  Indian Floral-Themed Arch Linux Distribution"
echo "============================================="
echo ""

# Clean previous build
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR" "$ISO_DIR" "$OUT_DIR"

echo "[1/5] Verifying build environment..."
command -v pacman >/dev/null 2>&1 || { echo "Error: pacman not found. This build requires Arch Linux."; exit 1; }
command -v mkarchiso >/dev/null 2>&1 || { echo "Installing archiso..."; sudo pacman -S --noconfirm archiso; }
command -v go >/dev/null 2>&1 || { echo "Installing Go toolchain..."; sudo pacman -S --noconfirm go; }

echo "[2/5] Preparing ISO profile..."
PROFILE="$SCRIPT_DIR/installation/iso"
STAGED="$BUILD_DIR/staged-profile"
rm -rf "$STAGED"
mkdir -p "$STAGED"

# Copy profile components
for item in profiledef.sh packages.x86_64 pacman.conf airootfs efiboot syslinux grub; do
  [ -e "$PROFILE/$item" ] && cp -a "$PROFILE/$item" "$STAGED/"
done

echo "[3/5] Building installer components..."

# Build the TUI installer
TUI_DIR="$SCRIPT_DIR/installation/tui"
if [ -f "$TUI_DIR/go.mod" ]; then
  echo "  Building banding-tui..."
  mkdir -p "$STAGED/airootfs/usr/local/bin"
  (cd "$TUI_DIR" && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w -buildid=' -o "$STAGED/airootfs/usr/local/bin/banding-tui" . 2>/dev/null) || echo "  TUI build skipped (no Go module)"
fi

echo "[4/5] Packaging desktop components..."
# Stage the desktop payload
PAYLOAD_DIR="$STAGED/airootfs/usr/share/banding-india-flovers"
mkdir -p "$PAYLOAD_DIR"
cp -a "$SCRIPT_DIR"/ryoku/* "$PAYLOAD_DIR/" 2>/dev/null || true
cp -a "$SCRIPT_DIR"/system/* "$PAYLOAD_DIR/" 2>/dev/null || true

# Brand assets
mkdir -p "$PAYLOAD_DIR/brand"
cp -a "$SCRIPT_DIR/ryoku/assets/brand/"* "$PAYLOAD_DIR/brand/" 2>/dev/null || true

echo "[5/5] Building ISO image..."

# Check if running as root or has sudo
if [ "$EUID" -eq 0 ]; then
  mkarchiso -v -w "$BUILD_DIR/mkarchiso-work" -o "$OUT_DIR" "$STAGED"
else
  sudo mkarchiso -v -w "$BUILD_DIR/mkarchiso-work" -o "$OUT_DIR" "$STAGED"
fi

echo ""
echo "============================================="
echo "  Build Complete!"
echo "============================================="
echo ""
echo "ISO output: $OUT_DIR/"
ls -lh "$OUT_DIR/"*.iso 2>/dev/null || echo "  (ISO file will be here after build)"
echo ""
echo "SHA256 checksums:"
(cd "$OUT_DIR" && sha256sum -- *.iso 2>/dev/null) || true
echo ""
echo "Brand assets:"
echo "  Logo:     $SCRIPT_DIR/ryoku/assets/brand/logo.svg"
echo "  Mark:     $SCRIPT_DIR/ryoku/assets/brand/logo-mark.svg"
echo "  ASCII:    $SCRIPT_DIR/ryoku/assets/brand/logo.txt"
echo ""
echo "To test in QEMU:"
echo "  qemu-system-x86_64 -cdrom $OUT_DIR/*.iso -m 4G -enable-kvm"
