# Security Policy

ddez handles Datadog credentials (API/application keys, access tokens). By
design they live only in the OS keychain or in environment variables — never
in the config file, never in logs. If you find a way to make ddez leak a
credential, that is a security bug.

## Reporting

Use GitHub's **private vulnerability reporting** on this repository
(Security → Report a vulnerability). Please do not open public issues for
suspected credential leaks, and never paste real keys or tokens into an
issue or PR.

Supported version: the latest release only.
