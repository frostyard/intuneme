# Installation

Choose the method that fits your workflow.

## Package manager (deb / rpm)

Pre-built packages are available on the [GitHub releases page](https://github.com/frostyard/intuneme/releases).

=== "Debian/Ubuntu"

    Download the `.deb` package from the latest release and install it:

    ```bash
    # Replace X.Y.Z with the version you want
    curl -LO https://github.com/frostyard/intuneme/releases/latest/download/frostyard-intuneme_X.Y.Z_amd64.deb
    sudo dpkg -i frostyard-intuneme_X.Y.Z_amd64.deb
    ```

=== "Fedora/RHEL"

    Download the `.rpm` package from the latest release and install it:

    ```bash
    # Replace X.Y.Z with the version you want
    curl -LO https://github.com/frostyard/intuneme/releases/latest/download/frostyard-intuneme-X.Y.Z-1.x86_64.rpm
    sudo rpm -i frostyard-intuneme-X.Y.Z-1.x86_64.rpm
    ```

## go install

If you have Go 1.26 or later installed, you can install directly from source:

```bash
go install github.com/frostyard/intuneme@latest
```

This places the `intuneme` binary in `$GOPATH/bin` (typically `~/go/bin`). Make sure that directory is on your `$PATH`.

`intuneme init` is the explicit, sudo-authenticated install and will copy the running binary to the root-owned helper path even when the source is under your home directory. Later `intuneme start` runs will not silently replace that root-owned helper from a user-owned binary. To let automatic helper updates work after a CLI upgrade, install intuneme to a root-owned path:

```bash
sudo install -m 0755 "$(go env GOPATH)/bin/intuneme" /usr/local/bin/intuneme
```

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

## Applying the security upgrade

Install the updated CLI in a root-owned path, then run `intuneme start`. It asks
for sudo authorization and refreshes security policy even when the container is
already running. If migration fails, resolve the reported error and rerun it.
Development binaries in user-owned paths cannot refresh privileged helpers.
For an older running installation without the new machine helper, `intuneme stop`
and `intuneme recreate` use an authenticated stop fallback.
