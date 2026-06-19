# Security Policy

## Supported versions

Security updates target the latest commit on `main` until this project has formal version support.

## Reporting a vulnerability

Please do not open a public issue for security-sensitive reports.

Instead, report vulnerabilities privately through GitHub's private vulnerability reporting feature if it is enabled for the repository. If that is not available, contact the maintainer directly and include:

- affected version or commit
- operating system and shell/tmux environment
- steps to reproduce
- expected and actual behavior
- any relevant logs with secrets removed

## Sensitive data

`amux` stores workspace metadata in a TSV file. Do not put secrets in workspace names, tmux window names, workdirs, or thread identifiers. If you share configs in issues or pull requests, replace real Amp thread IDs/URLs and private paths with placeholders.
