# Contributing to kubevirt-metrics-exporter

Thank you for your interest in contributing!

## Building and testing

Prerequisites: Go 1.25+, clang, llvm, libbpf-devel

```bash
make build        # generates eBPF bindings and builds the binary (requires clang/llvm)
make test         # runs all tests (requires generated eBPF bindings)
make test-unit    # runs unit tests for non-eBPF packages (works on macOS)
make image        # builds container image with podman (includes full toolchain)
```

To build and push a custom image:

```bash
make push IMAGE=quay.io/myuser/kubevirt-metrics-exporter TAG=v0.1.0
```

See [deployment](docs/deployment.md#from-source) to deploy a local build.

## Development workflow

1. Fork the repository and create a feature branch
2. Make your changes and add tests where appropriate
3. Run `make test` and `make lint`
4. Open a pull request with a clear description

## Pull requests

- Keep PRs focused — one feature or fix per PR
- Ensure tests and lint pass before submitting
- Use imperative commit messages ("Add metric" not "Added metric")

## Contributor compliance with Developer Certificate of Origin (DCO)

We require every contributor to certify that they are legally permitted to
contribute to this project. A contributor expresses this by signing their
commits, thereby stating compliance with the
[Developer Certificate of Origin](https://developercertificate.org/).

A signed commit includes a line like the following in the commit message:

```
Signed-off-by: Jane Doe <jane.doe@example.com>
```

Add `--signoff` to your git commit command, or use `git commit -s`.

## License

By contributing, you agree that your contributions will be licensed under the
Apache License, Version 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
