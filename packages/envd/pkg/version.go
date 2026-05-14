package pkg

// Version is the envd build identifier exposed via runtime.v1.RuntimeService/GetVersion.
//
// Default reflects MaxiCore's current B.II.1d-wire iteration (semver-valid pre-release
// against the upstream e2b base 0.5.x). Production builds may override via ldflags:
//
//	go build -ldflags "-X 'github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/pkg.Version=0.8.0-b.ii.1d-wire+abc1234'"
//
// Bumped from `const` to `var` so the linker can inject the value — required for
// observability (B.II.1d-wire-version-bump 2026-05-14, fixes bugbot-forensik P2:
// GetVersion was reporting upstream "0.5.18" while binary actually contained the
// runtime.v1.* + HMAC-V2 + WebdevService 0.8.0 implementation).
//
// Semver note: `0.8.0-b.ii.1d-wire` is valid SemVer 2.0 (pre-release identifiers
// are dot-separated `[0-9A-Za-z-]+`). It remains >= MinEnvdVersionForSnapshot
// ("0.5.0") for the shared snapshot-compatibility check.
var Version = "0.8.0-b.ii.1d-wire"
