# Device Hotplug

intuneme automatically forwards USB security keys and video capture devices into the running container via udev hotplug rules. Devices are passed through when plugged in and cleaned up when removed.

## How it works

When the container is running, udev rules on the host detect supported devices and use `nsenter` to create the corresponding device nodes inside the container. The rules are installed by `intuneme start` and removed by `intuneme stop`.

## Supported devices

### YubiKey (USB security keys)

Yubico devices (USB vendor ID `1050`) are detected regardless of which physical port they are plugged into. This covers all YubiKey 5 series and other Yubico hardware keys.

Both the USB device node (`/dev/bus/usb/*`) and the HID raw device (`/dev/hidraw*`) are forwarded so tools like `ykman` and FIDO2 authentication work correctly inside the container.

### Webcams and video devices

V4L2 video devices (`/dev/video*`) and media controller nodes (`/dev/media*`) are forwarded automatically. This covers built-in laptop cameras and USB webcams.

!!! tip
    Hot-plugging the camera works seamlessly — you can dock and undock your laptop without restarting the container. The camera appears inside the container when connected and is removed when disconnected.

## Device lifecycle

| Event | Behaviour |
|---|---|
| Container starts with device already connected | Device is detected and forwarded at boot |
| Device plugged in while container is running | Device is forwarded immediately via udev |
| Device unplugged | Device node is removed from inside the container |
| Container stops | All udev rules are removed |

## Manual rule management

The rules are managed automatically, but you can install or remove them independently if needed (for example, to recover after a crash without restarting the container):

```bash
# Install rules without starting the container
intuneme udev install

# Remove rules without stopping the container
intuneme udev remove
```

Both commands are idempotent — `udev remove` succeeds even if no rules are currently installed.

## Troubleshooting

**Webcam not available in Teams or Edge**

```bash
# Check the udev rules are present
ls /etc/udev/rules.d/70-intuneme-video.rules

# Re-install if missing
intuneme udev install

# Verify the camera is visible on the host
ls /dev/video*

# Check hotplug forwarding logs
journalctl -t intuneme-hotplug
```

**YubiKey not detected inside the container**

```bash
# Check the udev rules are present
ls /etc/udev/rules.d/70-intuneme-yubikey.rules

# Re-install if missing
intuneme udev install

# Verify the key is detected on the host
lsusb | grep Yubico

# Check hotplug forwarding logs
journalctl -t intuneme-hotplug
```
