#!/usr/bin/env bash
# fix-selinux.sh — Fix intuneme shell access on SELinux-enforcing systems (Fedora, Bazzite, etc.)
#
# Three issues prevent 'intuneme shell' from working out of the box:
#
#   1. The polkit rule installed by 'intuneme init' checks for the "sudo" group,
#      but Fedora/RHEL-based systems use "wheel" instead.
#
#   2. The rootfs lives under ~/.local/share/ with data_home_t SELinux label,
#      which systemd-machined is not permitted to read.
#
#   3. systemd-machined lacks SELinux permissions to open and use PTY devices
#      (user_devpts_t) needed for an interactive shell session.
#
# Usage: bash fix-selinux.sh [--rootfs <path>]

set -euo pipefail

ROOTFS="${HOME}/.local/share/intuneme/rootfs"
POLKIT_RULE="/etc/polkit-1/rules.d/50-intuneme.rules"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --rootfs) ROOTFS="$2"; shift 2 ;;
        *) echo "Unknown option: $1" >&2; exit 1 ;;
    esac
done

if [[ ! -d "$ROOTFS" ]]; then
    echo "Error: rootfs not found at $ROOTFS" >&2
    echo "Run 'intuneme init' first, or pass --rootfs <path>" >&2
    exit 1
fi

echo "==> Fixing polkit rule (adding wheel group support)..."
sudo tee "$POLKIT_RULE" > /dev/null <<'EOF'
polkit.addRule(function(action, subject) {
    if ((action.id == "org.freedesktop.machine1.manage-machines" ||
         action.id == "org.freedesktop.machine1.manage-images" ||
         action.id == "org.freedesktop.machine1.login" ||
         action.id == "org.freedesktop.machine1.shell" ||
         action.id == "org.freedesktop.machine1.host-shell") &&
        (subject.isInGroup("sudo") || subject.isInGroup("wheel"))) {
        return polkit.Result.YES;
    }
});
EOF

echo "==> Relabeling rootfs as container_file_t..."
sudo semanage fcontext -a -t container_file_t "${ROOTFS}(/.*)?" 2>/dev/null || true
sudo restorecon -RF "$ROOTFS"

echo "==> Installing SELinux policy module for machinectl PTY access..."
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

cat > "$TMPDIR/intuneme-machined.te" <<'EOF'
module intuneme-machined 1.0;

require {
    type systemd_machined_t;
    type user_devpts_t;
    type user_tmp_t;
    class chr_file { open read write ioctl getattr };
    class lnk_file { read };
}

allow systemd_machined_t user_devpts_t:chr_file { open read write ioctl getattr };
allow systemd_machined_t user_tmp_t:lnk_file { read };
EOF

checkmodule -M -m -o "$TMPDIR/intuneme-machined.mod" "$TMPDIR/intuneme-machined.te"
semodule_package -o "$TMPDIR/intuneme-machined.pp" -m "$TMPDIR/intuneme-machined.mod"
sudo semodule -X 300 -i "$TMPDIR/intuneme-machined.pp"

echo ""
echo "Done. Try 'intuneme shell' now."
