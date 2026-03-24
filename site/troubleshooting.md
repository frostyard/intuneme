# Troubleshooting

Common issues and solutions. Click an item to expand it.

??? question "`intuneme shell` fails on Fedora/Bazzite (SELinux)"
    `intuneme init` handles this automatically: it relabels the rootfs as `container_file_t` and installs a policy module that grants `systemd-machined` the PTY access it needs for `machinectl shell`.

    If you upgraded from an older install and did not re-run `init`, use the provided script:

    ```bash
    bash scripts/fix-selinux.sh
    ```

    Alternatively, re-run initialization:

    ```bash
    intuneme init --force
    ```

??? question "intune-portal crashes with \"No authorization protocol specified\""
    The Xauthority file is not being forwarded into the container. Check that your host has an Xauthority file in `$XAUTHORITY` or `/run/user/$UID/`.

    Common file name patterns to look for:

    ```bash
    ls /run/user/$UID/.mutter-Xwaylandauth.* /run/user/$UID/xauth_* 2>/dev/null
    ```

    If no file is found, your compositor may not be creating one. Try restarting your graphical session.

??? question "intune-portal shows error 1001 or \"UI web navigation failed\""
    The Microsoft Identity broker services are not running inside the container. Open a shell and check their status:

    ```bash
    intuneme shell
    ```

    Inside the container:

    ```bash
    sudo systemctl status microsoft-identity-device-broker
    systemctl --user status microsoft-identity-broker
    ```

    If either service has failed, restart it:

    ```bash
    sudo systemctl restart microsoft-identity-device-broker
    systemctl --user restart microsoft-identity-broker
    ```

??? question "Compliance check fails"
    The intune-agent timer may not be running. Inside the container:

    ```bash
    intuneme shell
    ```

    Then:

    ```bash
    systemctl --user start intune-agent.timer
    /opt/microsoft/intune/bin/intune-agent
    ```

    Running the agent directly will output diagnostic information if it encounters an error.

??? question "Edge crashes with \"Trace/breakpoint trap\""
    This is almost always a corrupted browser profile, not a GPU or sandbox issue.

    First, test with a fresh profile:

    ```bash
    microsoft-edge --user-data-dir=/tmp/edge-test
    ```

    If Edge launches successfully with a fresh profile, the issue is the existing profile. Move it aside and restart Edge:

    ```bash
    mv ~/.config/microsoft-edge ~/.config/microsoft-edge.bak
    ```

    Edge will create a new profile on next launch.

??? question "No sound in Edge or other container apps"
    Check that PipeWire is forwarded. The host needs a PipeWire socket at `/run/user/$UID/pipewire-0`:

    ```bash
    ls -la /run/user/$UID/pipewire-0
    ```

    Inside the container, verify the `PIPEWIRE_REMOTE` environment variable is set:

    ```bash
    intuneme shell
    echo $PIPEWIRE_REMOTE
    ```

    If PipeWire is not running on the host, start it:

    ```bash
    systemctl --user start pipewire pipewire-pulse
    ```

??? question "Webcam not available in Teams or Edge"
    Check that udev rules are installed:

    ```bash
    ls /etc/udev/rules.d/70-intuneme-video.rules
    ```

    If the file is missing, install the rules:

    ```bash
    intuneme udev install
    ```

    Verify the camera is detected on the host:

    ```bash
    ls /dev/video*
    ```

    Check device forwarding logs for errors:

    ```bash
    journalctl -t intuneme-hotplug
    ```

??? question "YubiKey not detected inside the container"
    Check that udev rules are installed:

    ```bash
    ls /etc/udev/rules.d/70-intuneme-yubikey.rules
    ```

    If the file is missing, install the rules:

    ```bash
    intuneme udev install
    ```

    Verify the key is detected on the host:

    ```bash
    lsusb | grep Yubico
    ```

    Check device forwarding logs:

    ```bash
    journalctl -t intuneme-hotplug
    ```

    If the key was plugged in before the container started, it should have been forwarded automatically at boot. If not, unplug and re-plug the key while the container is running to trigger the hotplug rules.
