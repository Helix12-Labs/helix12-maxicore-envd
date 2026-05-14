# Modifications from upstream e2b-dev/infra

This file documents all changes made by Helix12-Labs to the upstream
`e2b-dev/infra` source code (commit `7f3e661c8c1da50c3be5f34ca502c4f2fbe7d4c4`,
2026-05-14). Apache-2.0 license compliance per LICENSE + NOTICE.

## 1. Subset extraction (2026-05-14)

Only the following directories were forked:

- `packages/envd/` (full, 91 Go files, ~1.1 MB)
- `packages/shared/pkg/filesystem/`
- `packages/shared/pkg/httpserver/`
- `packages/shared/pkg/id/`
- `packages/shared/pkg/keys/`
- `packages/shared/pkg/smap/`
- `packages/shared/pkg/utils/`

The remaining `packages/{orchestrator,api,auth,clickhouse,...}` and other
`shared/pkg/*` packages were not forked. They are not used by envd at runtime.

## 2. Module path rename (2026-05-14, systematic)

Mechanical sed-replacement across 53 files:

- `github.com/e2b-dev/infra/packages/envd` → `github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd`
- `github.com/e2b-dev/infra/packages/shared` → `github.com/Helix12-Labs/helix12-maxicore-envd/packages/shared`

Affects:
- `go.mod` (module + replace directive)
- All `*.go` files import statements

No semantic code change — only the Go module path identifier.

**Note:** Protobuf-encoded package descriptors inside `packages/envd/internal/services/legacy/*.pb.go`
still contain the original `e2b-dev/infra` strings. These are wire-format protobuf
descriptors, not Go import paths. Changing them would break wire compatibility
with any peer code referencing the original descriptors. Preserved as-is.

## 3. Files added by Helix12-Labs (new, no upstream)

- `LICENSE` (Apache-2.0 — copied verbatim from upstream)
- `NOTICE` (new — required by Apache-2.0 §4(d))
- `README.md` (new — MaxiCore-specific orientation)
- `MODIFICATIONS.md` (this file)
- `.github/workflows/ci.yml` (TBD — MaxiCore CI pipeline)
- `Makefile.maxicore` (TBD — MaxiCore build entry-point)

## 4. Files NOT modified

Every file under `packages/envd/` and `packages/shared/` that was not subject
to the systematic module-path rename in section 2 above is verbatim from
upstream. Original copyright headers preserved.

## 5. Planned modifications (future sprints)

- **B.II.2c (MMDS-Integration):** May add MaxiCore-specific MMDS field validation
  to `packages/envd/internal/api/init.go`. Any such change WILL get a
  `// Modified by Helix12-Labs YYYY-MM-DD` header above the original copyright.
- **B.II.2a (vm-rootfs integration):** Adds new file `packages/envd/Dockerfile.maxicore`
  for MaxiCore-specific build. New file, no upstream conflict.

## 6. Upstream sync policy

Periodic rebase against `e2b-dev/infra` `main` planned. The systematic
module-path rename can be re-applied via `scripts/rename-module-path.sh` (TBD).
