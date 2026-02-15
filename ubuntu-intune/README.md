# ubuntu-intune
Container image for simplifying the use of [Intune](https://www.microsoft.com/en-us/security/business/microsoft-intune) on [SNOW Linux](https://github.com/frostyard/snosi) and other Linux distros with secure boot and full disk encryption via [intuneme](https://github.com/frostyard/intuneme).

This container is based on Ubuntu 24.04 LTS and includes:
* microsoft-identity-broker
* microsoft-edge-stable
* intune-portal
* unattended-upgrades enabled for all repos
* Supporting packages for Yubikeys
* Fixes for Intune services to run properly in a container
* Required PAM & security.d changes

Container images uploaded to the GHCR are signed with [cosign](https://github.com/sigstore/cosign) and can be validated with the `cosign.pub` file found [here](https://github.com/frostyard/intuneme/raw/refs/heads/main/ubuntu-intune/cosign.pub).

`cosign verify --key cosign.pub ghcr.io/frostyard/ubuntu-intune:latest`
