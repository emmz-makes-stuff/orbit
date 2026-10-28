# Dependencies

<!-- Maintained by /emmz:architecture; verified by `make deps`. -->

| Tool | Why | Version | Check | Auth check | Install / auth |
|---|---|---|---|---|---|
| go | build, test, fmt, vet | >=1.27 | `go version` | — | `brew install go` |
| staticcheck | lint gate | latest compatible with the Go toolchain | `staticcheck -version` | — | `go install honnef.co/go/tools/cmd/staticcheck@latest` |
| openspec | change tracking | >=1.15 | `openspec --version` | — | `npm i -g @fission-ai/openspec` |
| orbctl (OrbStack) | the VM provider orbit wraps: isolated machines, selective mounts, SSH agent forwarding, `ORBENV` | >=2.2 | `orbctl version` | `orbctl status` | `brew install orbstack`, then open OrbStack once |
| security (macOS) | stores and reads the Claude token in the Keychain | built in | `command -v security` | — | ships with macOS |
| claude | `claude setup-token` creates the long-lived token orbit injects | any with `setup-token` | `claude --version` | — | `curl -fsSL https://claude.ai/install.sh \| bash`; then `claude setup-token` once and give the token to `orbit` |
| ssh-agent with keys | git push from inside VMs via the forwarded agent | — | `test -n "$SSH_AUTH_SOCK"` | `ssh-add -l` | `ssh-add --apple-use-keychain ~/.ssh/<key>` |

Note: `go install` puts staticcheck in `$(go env GOPATH)/bin`. The `Makefile` prepends that to its PATH, so
`make lint` and `make deps` find it even when your shell's PATH doesn't include it.
