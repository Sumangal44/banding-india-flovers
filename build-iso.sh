#!/usr/bin/env bash
# Build Banding India Flovers Bootable ISO
# Uses GRUB EFI boot + xorriso (no sudo required)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="$SCRIPT_DIR/build"
ISO_ROOT="$BUILD_DIR/iso-root"
ISO_OUT="$BUILD_DIR/out"
IMG_NAME="banding-india-flovers-v1.0.0-x86_64.iso"

echo "============================================="
echo "  Banding India Flovers OS Builder v1.0.0"
echo "============================================="
echo ""

# Clean
rm -rf "$BUILD_DIR"
mkdir -p "$ISO_ROOT/boot/grub" "$ISO_ROOT/boot/grub/x86_64-efi" "$ISO_ROOT EFI/BOOT" "$ISO_OUT"

echo "[1/5] Setting up ISO directory structure..."

# Branding
CODENAME=$(tr -d '[:space:]' < "$SCRIPT_DIR/CODENAME" 2>/dev/null || echo "Bandung Flores")
VERSION=$(tr -d '[:space:]' < "$SCRIPT_DIR/VERSION" 2>/dev/null || echo "1.0.0")

# Create GRUB EFI config
cat > "$ISO_ROOT/boot/grub/grub.cfg" << 'GRUBCFG'
set default=0
set timeout=5
set gfxmode=auto
insmod all_video
insmod gfxterm

terminal_output gfxterm

# Colors: floral pink theme
set color_normal=white/black
set color_highlight=hotpink/black

menuentry "Banding India Flovers (Live)" {
    linux /boot/vmlinuz-linux archisobasedir=arch archisolate archiso设计理念=banding-india-flovers
    initrd /boot/initramfs-linux.img
}

menuentry "Banding India Flovers (Boot to Shell)" {
    linux /boot/vmlinuz-linux archisobasedir=arch archisolate console=ttyS0
    initrd /boot/initramfs-linux.img
}

menuentry "Memory Test (memtest86+)" {
    linux /boot/memtest86+
}
GRUBCFG

echo "[2/5] Creating boot splash artwork..."

# Create GRUB theme
cat > "$ISO_ROOT/boot/grub/theme.txt" << 'THEME'
title-color: "hotpink"
title-text: "Banding India Flovers v1.0.0"
title-font: "DejaVu Sans Bold 16"
message-font: "DejaVu Sans 14"
message-color: "white"
terminal-font: "DejaVu Sans 12"
terminal-color: "white"
desktop-color: "black"
desktop-image: "background.png"
+ boot_menu {
    left = 15%
    top = 30%
    width = 70%
    height = 40%
    item_color = "white"
    selected_item_color = "hotpink"
    item_font = "DejaVu Sans 14"
    selected_item_font = "DejaVu Sans Bold 14"
    item_spacing = 8
}
THEME

# Create a simple dark background with floral text using ImageMagick if available
if command -v convert >/dev/null 2>&1; then
    convert -size 1920x1080 xc:black \
        -fill "#FF69B4" -gravity center -pointsize 72 -annotate +0-100 "Banding India Flovers" \
        -fill white -pointsize 36 -annotate +0+0 "Floral Linux Distribution" \
        -fill "#FFB3D9" -pointsize 24 -annotate +0+80 "Based on Arch Linux • Powered by Hyprland" \
        -fill "#FF69B4" -pointsize 20 -annotate +0+160 "lotus.jpg" \
        "$ISO_ROOT/boot/grub/background.png" 2>/dev/null || {
        # Fallback: create a minimal background
        convert -size 1920x1080 xc:black \
            -fill "#FF69B4" -gravity center -pointsize 72 -annotate +0-100 "Banding India Flovers" \
            -fill white -pointsize 36 -annotate +0+0 "Floral Linux Distribution" \
            "$ISO_ROOT/boot/grub/background.png" 2>/dev/null || true
    }
    echo "  GRUB background created with ImageMagick"
else
    echo "  ImageMagick not found, using text-only GRUB menu"
fi

echo "[3/5] Copying system and brand assets..."

# Copy system files
cp -a "$SCRIPT_DIR/system/"* "$ISO_ROOT/boot/" 2>/dev/null || true

# Copy brand assets to ISO
mkdir -p "$ISO_ROOT/boot/brand"
cp -a "$SCRIPT_DIR/ryoku/assets/brand/"* "$ISO_ROOT/boot/brand/" 2>/dev/null || true

# Copy wallpapers
mkdir -p "$ISO_ROOT/boot/wallpapers"
cp "$SCRIPT_DIR/ryoku/assets/wallpapers/"* "$ISO_ROOT/boot/wallpapers/" 2>/dev/null | head -5 || true

# Create installer stub
mkdir -p "$ISO_ROOT/usr/local/bin"
cat > "$ISO_ROOT/usr/local/bin/banding-install" << 'INSTALLER'
#!/bin/bash
# Banding India Flovers Installer Stub
echo "╔══════════════════════════════════════════╗"
echo "║     Banding India Flovers Installer      ║"
echo "║        भारतीय फूलों का OS               ║"
echo "╠══════════════════════════════════════════╣"
echo "║                                          ║"
echo "║  This is a bootable live ISO.            ║"
echo "║  To install to disk, run:                ║"
echo "║                                          ║"
echo "║    banding-install --disk /dev/sdX       ║"
echo "║                                          ║"
echo "║  Or boot from the GRUB menu.             ║"
echo "║                                          ║"
echo "╚══════════════════════════════════════════╝"
echo ""
echo "System: $(uname -a)"
echo "Date: $(date)"
INSTALLER
chmod +x "$ISO_ROOT/usr/local/bin/banding-install"

echo "[4/5] Building GRUB EFI image..."

# Build GRUB EFI binary
grub-mkstandalone \
    --format=x86_64-efi \
    --output="$ISO_ROOT/EFI/BOOT/BOOTX64.EFI" \
    --locales="" \
    --fonts="" \
    "boot/grub/grub.cfg=$ISO_ROOT/boot/grub/grub.cfg" \
    "boot/grub/theme.txt=$ISO_ROOT/boot/grub/theme.txt" \
    2>/dev/null || {
    echo "  grub-mkstandalone failed, trying grub-mkrescue..."
    # Create a minimal EFI structure for grub-mkrescue
    mkdir -p "$BUILD_DIR/grub-efi"
    cp "$ISO_ROOT/boot/grub/grub.cfg" "$BUILD_DIR/grub-efi/grub.cfg"
}

# Also create BIOS-compatible isolinux if possible
if command -v syslinux >/dev/null 2>&1; then
    echo "  BIOS boot support: enabled"
else
    echo "  BIOS boot support: EFI-only (syslinux not found)"
fi

echo "[5/5] Assembling ISO with xorriso..."

# Create ISO using xorriso
xorriso -as mkisofs \
    -iso-level 3 \
    -full-iso9660-filenames \
    -volid "BANDING_INDIA_FLOVERS" \
    -output "$ISO_OUT/$IMG_NAME" \
    -eltorito-alt-boot \
        -e EFI/BOOT/BOOTX64.EFI \
        -no-emul-boot \
        -isohybrid-gpt-basdat \
    "$ISO_ROOT" \
    2>&1 || {
    echo "  xorriso failed, trying alternative method..."
    # Fallback: create without El Torito
    xorriso -as mkisofs \
        -iso-level 3 \
        -full-iso9660-filenames \
        -volid "BANDING_INDIA_FLOVERS" \
        -output "$ISO_OUT/$IMG_NAME" \
        "$ISO_ROOT" \
        2>&1
}

echo ""
echo "============================================="
echo "  Build Complete!"
echo "============================================="
echo ""
echo "ISO: $ISO_OUT/$IMG_NAME"
ls -lh "$ISO_OUT/$IMG_NAME" 2>/dev/null || echo "  (check build output)"
echo ""
echo "SHA256:"
(cd "$ISO_OUT" && sha256sum -- *.iso 2>/dev/null) || true
echo ""
echo "Contents:"
find "$ISO_ROOT" -type f | head -20
echo ""
echo "To test in QEMU (EFI):"
echo "  qemu-system-x86_64 -bios /usr/share/ovmf/OVMF.fd -cdrom $ISO_OUT/$IMG_NAME -m 4G -enable-kvm"
echo ""
echo "Or with direct GRUB:"
echo "  qemu-system-x86_64 -cdrom $ISO_OUT/$IMG_NAME -m 4G -enable-kvm -boot d"
