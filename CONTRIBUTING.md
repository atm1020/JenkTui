# Contributing

Thanks for contributing to JenkTui.

## Development setup

JenkTui requires Go 1.25.13 or newer. Install [Task](https://taskfile.dev/), then run:

```bash
task tools:install
task check
task build
```

The local Jenkins fixtures in `dev/docker/` are optional and are intended only
for development and testing.

## Pull requests

- Keep changes focused and explain user-visible behavior.
- Add or update tests for behavior changes.
- Run `task check` and `task build` before submitting.
- Use Conventional Commit messages, for example `feat: add ...` or `fix: ...`.
- Never commit Jenkins URLs containing credentials, API tokens, local databases,
  logs, or personal configuration files.

By contributing, you agree that your contributions may be distributed under
this project's [MIT License](LICENSE).
