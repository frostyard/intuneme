#!/bin/bash
# install-desktop-items.sh
# Installs .desktop entries for intuneme-managed apps so they appear in the
# GNOME application grid and are pinned to the dash.
#
# Usage: bash scripts/install-desktop-items.sh [--uninstall]

set -euo pipefail

APPS_DIR="${HOME}/.local/share/applications"
ICONS_DIR="${HOME}/.local/share/icons/hicolor"
INTUNEME_BIN="${INTUNEME_BIN:-intuneme}"
INTUNEME_ROOT="${INTUNEME_ROOT:-${HOME}/.local/share/intuneme}"
ROOTFS="${INTUNEME_ROOT}/rootfs"

EDGE_ID="intuneme-edge.desktop"
PORTAL_ID="intuneme-portal.desktop"
EDGE_DESKTOP="${APPS_DIR}/${EDGE_ID}"
PORTAL_DESKTOP="${APPS_DIR}/${PORTAL_ID}"

# Pin a .desktop ID to org.gnome.shell favorite-apps if not already present.
pin_app() {
    local id="$1"
    local current
    current=$(gsettings get org.gnome.shell favorite-apps 2>/dev/null || echo "[]")
    if echo "$current" | grep -qF "$id"; then
        return 0
    fi
    local new
    new=$(echo "$current" | sed "s/]$/, '${id}']/")
    gsettings set org.gnome.shell favorite-apps "$new"
}

# Remove a .desktop ID from org.gnome.shell favorite-apps.
unpin_app() {
    local id="$1"
    local current
    current=$(gsettings get org.gnome.shell favorite-apps 2>/dev/null || echo "[]")
    if ! echo "$current" | grep -qF "$id"; then
        return 0
    fi
    local new
    new=$(echo "$current" | sed "s/, '${id}'//g; s/'${id}', //g; s/'${id}'//g")
    gsettings set org.gnome.shell favorite-apps "$new"
}

# Copy icons for a given app name from the container rootfs hicolor theme to the host.
install_icons() {
    local icon_name="$1"
    local src_base="${ROOTFS}/usr/share/icons/hicolor"
    if [[ ! -d "$src_base" ]]; then
        echo "  Warning: rootfs icon directory not found at ${src_base}, skipping icons." >&2
        return 0
    fi
    local copied=0
    while IFS= read -r src; do
        local rel="${src#${src_base}/}"   # e.g. 256x256/apps/microsoft-edge.png
        local dest="${ICONS_DIR}/${rel}"
        mkdir -p "$(dirname "$dest")"
        cp "$src" "$dest"
        (( copied++ )) || true
    done < <(find "${src_base}" -name "${icon_name}.png" -o -name "${icon_name}.svg" 2>/dev/null)
    if [[ $copied -gt 0 ]]; then
        echo "  Installed ${copied} icon(s) for ${icon_name}"
    else
        echo "  Warning: no icons found for ${icon_name} in rootfs." >&2
    fi
}

uninstall=false
if [[ "${1:-}" == "--uninstall" ]]; then
    uninstall=true
fi

if $uninstall; then
    echo "Removing intuneme desktop entries..."
    rm -f "$EDGE_DESKTOP" "$PORTAL_DESKTOP"
    update-desktop-database "$APPS_DIR" 2>/dev/null || true
    echo "Removing icons..."
    find "${ICONS_DIR}" -name "microsoft-edge.png" -o -name "intune.png" 2>/dev/null | xargs rm -f
    gtk-update-icon-cache --force "${ICONS_DIR}" 2>/dev/null || true
    echo "Unpinning from dash..."
    unpin_app "$EDGE_ID"
    unpin_app "$PORTAL_ID"
    echo "Done. Entries removed."
    exit 0
fi

# Verify intuneme is on PATH
if ! command -v "$INTUNEME_BIN" &>/dev/null; then
    echo "Error: '$INTUNEME_BIN' not found on PATH." >&2
    echo "Install intuneme first, or set INTUNEME_BIN to its full path." >&2
    exit 1
fi

if [[ ! -d "$ROOTFS" ]]; then
    echo "Error: rootfs not found at ${ROOTFS}." >&2
    echo "Run 'intuneme init' first, or set INTUNEME_ROOT to the intuneme data directory." >&2
    exit 1
fi

mkdir -p "$APPS_DIR" "$ICONS_DIR"

IDENTITY="$("$INTUNEME_BIN" --root "$INTUNEME_ROOT" status | sed -n 's/^Identity: //p')"
if [[ "$IDENTITY" == "himmelblau-experimental" ]]; then
    rm -f "$EDGE_DESKTOP"
    unpin_app "$EDGE_ID"
    find "${ICONS_DIR}" -type f \( -name "microsoft-edge.png" -o -name "microsoft-edge.svg" \) -delete
    gtk-update-icon-cache --force "${ICONS_DIR}" 2>/dev/null || true
else
    echo "Installing Microsoft Edge icon..."
    install_icons "microsoft-edge"
    gtk-update-icon-cache --force "${ICONS_DIR}" 2>/dev/null || true
    cat > "$EDGE_DESKTOP" <<EOF
[Desktop Entry]
Version=1.0
Type=Application
Name=Microsoft Edge (Intune)
GenericName=Web Browser
Comment=Microsoft Edge running inside the Intune container
Exec="${INTUNEME_BIN}" "--root=${INTUNEME_ROOT}" open edge
Icon=microsoft-edge
StartupNotify=true
StartupWMClass=msedge
Categories=Network;WebBrowser;
Keywords=intune;edge;browser;microsoft;
EOF

    echo "Installed: $EDGE_DESKTOP"
fi

if [[ "$IDENTITY" == "himmelblau-experimental" ]]; then
    cat > "$PORTAL_DESKTOP" <<EOF
[Desktop Entry]
Version=1.0
Type=Application
Name=Himmelblau Sign-in
GenericName=Entra ID Sign-in
Comment=Start the experimental Himmelblau PAM sign-in and enrollment flow
Exec="${INTUNEME_BIN}" "--root=${INTUNEME_ROOT}" enroll
Icon=system-users
Terminal=true
StartupNotify=true
Categories=System;Security;
Keywords=himmelblau;entra;sign-in;enrollment;
EOF
else
    install_icons "intune"
    gtk-update-icon-cache --force "${ICONS_DIR}" 2>/dev/null || true
    cat > "$PORTAL_DESKTOP" <<EOF
[Desktop Entry]
Version=1.0
Type=Application
Name=Intune Portal
GenericName=Device Management
Comment=Microsoft Intune Portal running inside the Intune container
Exec="${INTUNEME_BIN}" "--root=${INTUNEME_ROOT}" open portal
Icon=intune
StartupNotify=true
Categories=System;Security;
Keywords=intune;portal;microsoft;mdm;compliance;
EOF
fi

echo "Installed: $PORTAL_DESKTOP"

# Refresh the desktop database so GNOME picks up the new entries immediately
update-desktop-database "$APPS_DIR" 2>/dev/null || true

# Pin to dash
echo "Pinning to dash..."
pin_app "$EDGE_ID"
pin_app "$PORTAL_ID"

echo ""
echo "Done. Edge and the identity sign-in shortcut are pinned to the dash and available in the app grid."
echo "If the container is not running when you launch them, you will"
echo "see an error — run 'intuneme start' first."
echo ""
echo "To remove: bash scripts/install-desktop-items.sh --uninstall"
