# Security Policy

## Supported versions

Only the latest `vX.Y.Z` release gets security fixes. The rolling `dev`
prerelease and older releases do not. A fix ships in a new release; upgrade to
it, then run `intuneme start` so that the privileged helpers and policies are
refreshed.

## Reporting a vulnerability

Use GitHub private vulnerability reporting:
https://github.com/frostyard/intuneme/security

Open the Security tab and click **Report a vulnerability**, or open the form
directly: https://github.com/frostyard/intuneme/security/advisories/new.

Do not open a public issue with the details of a vulnerability. Do not post
credentials in an issue, a pull request or a discussion.

## What to include

- The affected version or commit.
- The impact: what an attacker can do.
- The complete steps to reproduce it.

## Guidelines

- Avoid sending us executables.
- You can include any script you wrote and used, but avoid scripts that
  download and run binaries.
- We give priority to reports that show how the exploit works in a realistic
  environment.
- We prefer all communication in English.
- We do not offer financial rewards. We are happy to acknowledge your research
  publicly when possible.
