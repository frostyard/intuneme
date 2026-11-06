# Helper security

intuneme launches apps and MCP servers in the container through root-owned
helpers that `intuneme init` installs. This page says what those helpers allow.

App and MCP launches permanently disable setuid/file-capability elevation and
run as your configured numeric user. Container administration requiring sudo
must use an interactive `intuneme shell` session. The session restarts only the
Microsoft device broker through a scoped container D-Bus policy.

After upgrading, run `intuneme start` even if the container is already running.
It authenticates and refreshes the security policy without rebooting the
container. A failed policy update stops the command with an error.

Passwordless container shells are limited to your configured user and machine;
root shells require authentication. MCP mounts require Linux 5.12+ and accept a user-owned directory
and remain read-only. Install the CLI in a root-owned path before upgrading
with `intuneme start`; a user-owned development build cannot refresh privileged
helpers. Older running installations can still be stopped or recreated through
an authenticated fallback when the machine helper has not yet been installed.

See [Installation](../getting-started/installation.md) for the root-owned
install path and [Upgrading](../user-guide/upgrading.md) for `recreate`.
