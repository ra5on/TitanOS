# Contributing to TitanOS

Thanks for your interest in TitanOS! Issues, ideas and pull requests are welcome. Issues may be written in English or German.

## License

TitanOS is licensed under the **Functional Source License 1.1 with Apache 2.0 Future License (FSL-1.1-ALv2)**, see [LICENSE.md](LICENSE.md).

In short:

- You may use, modify and share TitanOS for any purpose **except a competing use**, i.e. offering TitanOS (or something substantially based on it) as a commercial product or service, such as selling it preinstalled on hardware or running it as a hosted service.
- Personal use, use inside a company, education and research are permitted.
- Two years after each version is released, that version automatically becomes available under the **Apache License 2.0**.

TitanOS is therefore *Fair Source*. Please do not describe it as "open source" until a version has converted.

Third-party components (Linux kernel, Debian packages, noVNC, xterm.js, …) keep their own licenses. See `THIRD_PARTY_LICENSES`.

## Contributor License Agreement (CLA)

Before an external contribution can be merged, its author must sign the
[TitanOS CLA](CLA.md). The agreement is currently a draft and the hosted CLA
Assistant is not configured yet. The maintainer must finish that setup and
review the agreement before accepting external code contributions. Bug reports
and discussion do not require a CLA.

Why: the CLA lets the maintainer distribute your contribution under the FSL today, under the Apache License 2.0 when a version converts, and under separate licenses if needed. You keep the copyright to your contribution.

## Reporting security issues

Please **do not** open a public issue for security vulnerabilities. Use GitHub's private vulnerability reporting ("Security" tab → "Report a vulnerability") instead.

## How to contribute

1. Open an issue first for larger changes, so we can agree on the approach before you invest time.
2. Fork the repository and create a branch from `main`.
3. Keep changes focused: one topic per pull request.
4. Run the tests before opening a pull request:
   ```
   make test
   ```
5. Describe what you changed and how you tested it. For UI changes, add a screenshot.

## Ground rules

- **Data safety comes first.** Anything that touches disks, RAID, filesystems or `/config` needs tests and an explicit confirmation step in the UI for destructive actions.
- No hardcoded colors in UI components; use the theme variables.
- User-facing text goes through the translation files (German and English).
- No vendor logos or third-party branding in the repository.
