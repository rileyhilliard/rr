# Security policy

## Reporting a vulnerability

If you discover a security vulnerability in Remote Runner, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email security concerns to the maintainer or open a private security advisory:

1. Go to the [Security Advisories](https://github.com/rileyhilliard/rr/security/advisories) page
2. Click "New draft security advisory"
3. Fill in the details of the vulnerability

You can expect an initial response within 48 hours. We'll work with you to understand the issue and coordinate a fix.

## Scope

Security issues we care about:

- Command injection vulnerabilities
- SSH key or credential exposure
- Lock file race conditions that could cause data corruption
- Path traversal in sync operations

## Supported versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | Yes                |

We only provide security fixes for the latest minor version.
