# Security Policy

## Supported Versions

Only the latest tagged release receives security fixes.

## Reporting a Vulnerability

Please **do not** open a public issue for security problems.

Report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/amhrmsn/go-logger/security/advisories/new)
so the issue can be triaged and fixed before public disclosure.

You can expect an initial response within 7 days.

## Scope Notes

go-logger is a logging library with no network I/O, no file I/O in the core
module, and zero third-party dependencies. The most security-relevant surface
is **redaction**: its documented limitations (record messages and the contents
of `slog.Any` values are not inspected) are by design and are not considered
vulnerabilities — see the `RedactionHandler` documentation. Reports about
sensitive data leaking through paths the documentation claims to protect are
very welcome.
