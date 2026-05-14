# maxicore-envd

In-VM agent (filesystem + process via Connect-RPC) for MaxiCore Firecracker sandboxes.

## Origin

This is a **fork of [e2b-dev/infra](https://github.com/e2b-dev/infra)** — specifically the `packages/envd` and selected `packages/shared/pkg/*` directories.

Forked at upstream commit `7f3e661c8c1da50c3be5f34ca502c4f2fbe7d4c4` on 2026-05-14.

License: Apache-2.0 (see [LICENSE](./LICENSE)). Copyright preservation: see [NOTICE](./NOTICE).

## Architecture

envd listens on `:49983` inside each Firecracker microVM and serves three protocols:

- **REST API** (OpenAPI 3.0): `/health`, `/metrics`, `/init`, `/envs`, `/files`
- **Connect-RPC Filesystem service**: Stat / MakeDir / Move / ListDir / Remove / WatchDir
- **Connect-RPC Process service**: List / Connect / Start / Update / StreamInput / SendSignal / CloseStdin

Authentication is multi-layered:

- `X-Access-Token` header (memguard-protected master token)
- HTTP Basic Auth username (Linux user resolution, password ignored)
- Signed URLs (`v1_<sha256-hex>`) for file ops without exposing the token
- MMDS-hash validation for `POST /init`

## Build

Requires Go 1.25+ and `make` (plus optional Rust for the FC fork build elsewhere).

```bash
cd packages/envd
make build           # → bin/envd (static linux/amd64)
```

The static binary is ~15 MB and runs as a systemd service `envd.service` inside the sandbox VM.

## MaxiCore Integration

- vm-rootfs bakes `envd` to `/usr/bin/envd` and installs `envd.service`
- sandbox-manager configures MMDS (`PUT /mmds`) with sandbox metadata + access-token hash before VM start
- Backend `_vm_curl` routes filesystem/process calls through envd's Connect-RPC endpoints (planned for B.II.2i)

## Layer in HELIX_12 stack

L2 (VM-Runtime) — same layer as vm-rootfs / vm-agent / vmm.

## Sprints

| Sprint | Status | Scope |
|--------|--------|-------|
| B.II.2 PREP | ✅ LIVE 2026-05-14 | Rust, NBD, MinIO, Hugepages, snapshot dirs |
| B.II.2a envd Fork | 🚧 in progress | this repo, vm-rootfs integration |
| B.II.2b FC v1.14-direct-mem | ⏸ pending | custom Firecracker fork |
| B.II.2c MMDS-Integration | ⏸ pending | sandbox-manager pre-VM-start hook |
| B.II.2d–h | ⏸ pending | snapshot, UFFD, NBD, layer-chain, warmpool |
| B.II.2i | ⏸ pending | Backend `_vm_curl` refactor to envd |

## Modifying files

If you change any file forked from e2b, prepend a comment:

```go
// Modified by Helix12-Labs 2026-05-14: <one-line reason>
```

Preserve the original copyright header above it. Apache-2.0 attribution is non-negotiable.

## Upstream tracking

To rebase against new upstream changes, run `scripts/rebase-upstream.sh` (TBD). For now, manual diff against `github.com/e2b-dev/infra` `main`.
