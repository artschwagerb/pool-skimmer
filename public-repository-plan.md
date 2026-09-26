# Public Repository Readiness Plan

## Goal

Prepare Pool Skimmer for a safe, reproducible public `v0.1.0` release without
changing its provider-neutral SCIM scope. Linux and macOS on amd64 and arm64 are
supported; Windows packaging remains out of scope.

## Current baseline

- The application is implemented in Go as the `pool-skimmer` binary.
- Running the binary without arguments opens the TUI; explicit CLI commands
  remain available for scripts and automation.
- Tests and `go vet` run in the Docker build.
- The runtime image is minimal, non-root, and contains only the executable and
  CA certificates.
- Release builds currently produce raw Linux and macOS binaries for amd64 and
  arm64 under `dist/`.
- GitHub Actions definitions now cover pull-request validation and tagged
  release archives, checksums, artifact attestations, and GitHub Releases. They
  cannot be proven in GitHub until the repository is initialized and pushed.
- `.env` files, build output, and `.DS_Store` files are ignored.
- A lightweight scan found no exposed credentials in the current working
  files. There is not yet a Git repository or Git history to audit.
- A local `govulncheck` attempt could not download the Go vulnerability index
  because the local network's TLS interception certificate was not trusted by
  the disposable container. Vulnerability status is therefore not yet
  confirmed.

## Decisions required

- [ ] Select the GitHub owner or organization and final repository URL.
- [ ] Select the open-source license. MIT is the recommended default for this
  small command-line tool unless a different policy applies.
- [ ] Confirm the GitHub Container Registry namespace, normally
  `ghcr.io/<owner>/pool-skimmer`.
- [ ] Decide whether the first release will be `v0.1.0` and whether releases
  require signed tags in addition to GitHub artifact attestations.

## Phase 1: blockers before the first public push

### 1. Establish the repository identity

- [ ] Initialize Git with an appropriate default branch.
- [ ] Change the module declaration from `module pool-skimmer` to
  `module github.com/<owner>/pool-skimmer`.
- [ ] Update internal imports to use the canonical module path.
- [ ] Confirm `go mod tidy`, tests, vet, Docker builds, and release builds still
  pass after the module rename.
- [ ] Remove the existing `.DS_Store` files before creating the initial commit,
  even though they are ignored.

Done when the repository builds only with the canonical public module path and
the initial staged file list contains no editor, operating-system, credential,
or build artifacts.

### 2. Add licensing and public project identity

- [ ] Add a root `LICENSE` file with the selected license and copyright owner.
- [ ] Add a concise README notice that Pool Skimmer is an independent,
  provider-neutral SCIM client and is not affiliated with or endorsed by
  WorkOS, Okta, or any other provider.
- [ ] Review dependency licenses for compatibility with the selected project
  license and record any required notices.

Done when visitors can clearly determine how the code may be used and that the
project is not an official provider product.

### 3. Protect credentials in transit

- [ ] Require `https://` SCIM endpoints by default.
- [ ] Add an explicit `--allow-insecure-http` escape hatch only for local
  development and test endpoints.
- [ ] Restrict the escape hatch to loopback hosts unless a future use case is
  deliberately reviewed and documented.
- [ ] Print a clear error without including the API key when insecure transport
  is rejected.
- [ ] Add tests covering HTTPS acceptance, HTTP rejection, the loopback
  override, embedded credentials, and newline/header injection.
- [ ] Keep API keys out of logs, errors, dry-run output, process arguments, and
  generated diagnostics.

Done when Pool Skimmer cannot accidentally send an API key over a remote plain
HTTP connection.

### 4. Audit the exact initial commit

- [ ] Review `git status --short` and `git diff --cached` before the first
  commit.
- [ ] Run a credential scanner against all staged files.
- [ ] Confirm example endpoints and keys are unmistakably synthetic.
- [ ] Confirm `.env`, local configuration, `dist/`, test coverage, and TUI
  captures containing real directory data are not tracked.
- [ ] After committing, scan the complete reachable Git history before pushing.

Done when both the staged tree and complete Git history pass the secret scan.

## Phase 2: public launch quality

### 5. Add continuous integration

Create a GitHub Actions workflow for pull requests and pushes that runs:

- [x] `gofmt` verification.
- [x] `go test ./...`.
- [x] `go test -race ./...` on a supported Linux runner.
- [x] `go vet ./...`.
- [x] `govulncheck ./...` using the official Go vulnerability database.
- [x] A runtime Docker image build.
- [x] A release-build smoke test for the four supported OS/architecture pairs.

Workflow requirements:

- [x] Pin third-party actions to full commit SHAs.
- [x] Grant only the permissions each job needs.
- [x] Do not make repository secrets available to pull-request validation.
- [x] Use dependency caching only where it measurably improves execution time.
- [x] Keep the Go version used by local builds, CI, and Docker documented and
  intentionally aligned.

Done when a clean pull request proves formatting, tests, vetting, vulnerability
status, image construction, and supported builds without provider credentials.

### 6. Automate tagged releases

Use GoReleaser or an equivalently reproducible release workflow triggered by
`v*` tags.

- [x] Build Linux and macOS binaries for amd64 and arm64.
- [x] Package binaries in consistently named `.tar.gz` archives.
- [x] Publish SHA-256 checksums.
- [ ] Generate an SBOM for release artifacts and the container image.
- [x] Generate GitHub artifact attestations or equivalent provenance.
- [x] Publish release notes and assets to a GitHub Release.
- [ ] Build and publish a multi-architecture image to
  `ghcr.io/<owner>/pool-skimmer`.
- [ ] Tag images with the release version and an intentional moving tag, if one
  is desired. Avoid relying only on `latest`.
- [ ] Embed the version, commit, and build date in the binary so `version`
  reports traceable release metadata.
- [ ] Add OCI image labels for source URL, version, revision, license, title,
  and description.
- [x] Keep publishing permissions restricted to the release jobs.

Done when a tag produces verifiable archives, checksums, provenance, a GitHub
Release, and a multi-architecture GHCR image without a manual local build.

### 7. Complete the public documentation

Expand `README.md` with:

- [ ] Installation from GitHub Releases.
- [ ] A canonical `go install github.com/<owner>/pool-skimmer/cmd/pool-skimmer@latest`
  example after the module path is updated.
- [ ] Docker and GHCR usage examples.
- [x] A sanitized screenshot or short recording of the TUI.
- [x] A support matrix for Linux/macOS and amd64/arm64.
- [x] A provider-compatibility statement explaining that SCIM filters, PATCH
  behavior, schemas, and authorization scopes vary.
- [x] A concise capability overview for the TUI and command-based interface.
- [x] Secure credential setup using named endpoint profiles, environment
  variables, key files, and the interactive prompt.
- [ ] Project status, support expectations, and the unofficial-project notice.
- [ ] CI and release badges only after their workflows exist and are stable.

Add public community files:

- [ ] `SECURITY.md` with a private vulnerability-reporting path, supported
  versions, and a warning not to disclose credentials or directory data in
  public issues.
- [ ] `CONTRIBUTING.md` with setup, formatting, tests, Docker validation,
  command/TUI behavior expectations, and pull-request guidance.
- [ ] Issue forms for bugs and feature requests that explicitly request
  sanitized logs and provider details.
- [ ] A pull-request template with testing, documentation, compatibility, and
  credential-safety checks.
- [ ] Optionally add `CODE_OF_CONDUCT.md` if outside contributions are actively
  invited.

Done when a new user can install, configure, evaluate, and safely report a
problem without needing unpublished project knowledge.

### 8. Add dependency and supply-chain maintenance

- [ ] Configure Dependabot for Go modules, Docker base images, and GitHub
  Actions, or document the selected equivalent.
- [ ] Schedule `govulncheck` in addition to running it on pull requests.
- [ ] Review whether Docker base images should be pinned by digest while still
  receiving automated update pull requests.
- [ ] Document how maintainers update dependencies and respond to vulnerability
  reports.
- [ ] Keep `go.sum` committed and review unexpected transitive dependency
  changes.

Done when dependencies, build actions, and base images have a visible and
repeatable update path.

## Phase 3: post-launch hardening and scalability

These items should not block the first release unless testing shows they affect
the intended initial users.

### 9. Scale the TUI for large directories

- [ ] Replace unconditional load-all startup behavior with pagination or lazy
  loading.
- [ ] Keep filtering and selection responsive while remote pages load.
- [ ] Provide progress, cancellation, retry, and partial-error states.
- [ ] Add configurable limits where a provider cannot support efficient
  pagination or filtering.
- [ ] Test representative large user and group collections without including
  real directory data in fixtures.

### 10. Discover provider capabilities

- [ ] Read SCIM `ServiceProviderConfig`, schemas, and resource types when the
  provider exposes them.
- [ ] Disable or explain actions the endpoint declares unsupported, especially
  PATCH and filtering.
- [ ] Treat discovery as advisory when providers return incomplete or
  inaccurate capability metadata.
- [ ] Preserve provider-specific attributes and the existing generic JSON
  inspection path.

### 11. Improve operational diagnostics

- [ ] Ensure Ctrl-C and context cancellation stop in-flight requests cleanly.
- [ ] Keep errors useful while redacting authorization values and sensitive
  payloads.
- [ ] Include provider request IDs in errors when available.
- [ ] Consider an opt-in diagnostic mode that reports methods, paths, timing,
  status codes, and retry decisions without credentials or resource bodies.

## Definition of public `v0.1.0` readiness

- [ ] All Phase 1 items are complete.
- [ ] CI is green from a clean checkout.
- [ ] `govulncheck` completes successfully or every reported reachable finding
  has a documented resolution.
- [ ] Release automation produces the four supported binary targets,
  checksums, provenance, and the GHCR image.
- [ ] Installation and first-run instructions have been tested exactly as
  published.
- [ ] `SECURITY.md`, `CONTRIBUTING.md`, and the selected license are present.
- [ ] No real API keys, endpoints, user data, group data, or identifying TUI
  captures exist in the working tree or Git history.
- [ ] CLI and TUI destructive actions still require confirmation and support
  concurrency protection where the provider permits it.
- [ ] The final image runs as a non-root user and contains no build toolchain.

## Reference material

- [Go module file reference](https://go.dev/doc/modules/gomod-ref)
- [Go vulnerability management](https://go.dev/doc/security/vuln/)
- [GitHub community health files](https://docs.github.com/en/communities/setting-up-your-project-for-healthy-contributions/creating-a-default-community-health-file)
- [GitHub Docker image publishing](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)
- [GitHub Container Registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
- [GoReleaser with GitHub Actions](https://goreleaser.com/ci/actions/)
- [GoReleaser artifact attestations](https://goreleaser.com/customization/attestations/)
