# Installation

Choose the method that fits your workflow.

## Package manager (deb / rpm)

Pre-built packages are available on the [GitHub releases page](https://github.com/frostyard/intuneme/releases).

=== "Debian/Ubuntu"

    Download the `.deb` package from the latest release and install it:

    ```bash
    # Replace X.Y.Z with the version you want
    curl -LO https://github.com/frostyard/intuneme/releases/latest/download/intuneme_X.Y.Z_linux_amd64.deb
    sudo dpkg -i intuneme_X.Y.Z_linux_amd64.deb
    ```

=== "Fedora/RHEL"

    Download the `.rpm` package from the latest release and install it:

    ```bash
    # Replace X.Y.Z with the version you want
    curl -LO https://github.com/frostyard/intuneme/releases/latest/download/intuneme_X.Y.Z_linux_amd64.rpm
    sudo rpm -i intuneme_X.Y.Z_linux_amd64.rpm
    ```

## go install

If you have Go 1.26 or later installed, you can install directly from source:

```bash
go install github.com/frostyard/intuneme@latest
```

This places the `intuneme` binary in `$GOPATH/bin` (typically `~/go/bin`). Make sure that directory is on your `$PATH`.

## Build from source

Clone the repository and build with `make`:

```bash
git clone https://github.com/frostyard/intuneme.git
cd intuneme
make install
```

`make install` builds the binary and installs it to `/usr/local/bin/intuneme`.

To build without installing:

```bash
make build
# binary at ./intuneme
```

## Verify

After installation, confirm the binary is accessible:

```bash
intuneme --version
```

## Next step

Once intuneme is installed, follow the [Quick Start](quick-start.md) guide to provision and boot the container.
