# intuneme
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/frostyard/intuneme/badge)](https://scorecard.dev/viewer/?uri=github.com/frostyard/intuneme)

`intuneme` provisions and manages a [systemd-nspawn](https://www.freedesktop.org/software/systemd/man/systemd-nspawn.html) container running Microsoft Intune on an immutable Linux host. The container handles enrollment, compliance, and corporate resource access while making minimal changes to the host.

## Features

- Container lifecycle management (`init`, `start`, `stop`, `destroy`, `recreate`)
- Broker proxy for host-side single sign-on (Edge, VS Code, and other MSAL apps)
- Device hotplug — YubiKey and webcam passthrough via udev rules
- GNOME Quick Settings extension for start/stop and app launch without a terminal
- Desktop shortcuts for Microsoft Edge and Intune Portal

## Documentation

Full documentation is at **https://frostyard.github.io/intuneme/**

## Quick install

```bash
go install github.com/frostyard/intuneme@latest
```

## Contributing

See the [contributing guide](https://frostyard.github.io/intuneme/contributing/) on the docs site.

## License

MIT
