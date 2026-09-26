# Repository Guidelines

## Product identity and scope

- The project, executable, container image, release artifact, and distribution
  package are named `pool-skimmer`. Do not publish a generic `scim` executable
  or package that could conflict with other SCIM tooling.
- Pool Skimmer is a provider-neutral SCIM 2.0 user and group management tool.
  WorkOS, Okta, and OpenAI may appear in examples or troubleshooting context,
  but their private APIs and workflows do not belong in the SCIM client.
- Supported resource operations include listing, inspection, creation, PATCH,
  replacement, deletion, user activation, group rename, and group membership
  management. Preserve this command-based interface for automation.
- Do not add WorkOS directory administration, Okta Push Groups configuration,
  NetID behavior, or application-specific group-linking logic.
- A successful SCIM `displayName` update changes the source SCIM resource only.
  It does not prove that Okta imported-app groups or another downstream target
  refreshed or linked the new name. Diagnose those systems independently.

## Language and repository layout

- Use Go for all application code. Do not reintroduce the removed Python
  implementation or Python packaging.
- Keep `cmd/pool-skimmer` thin. Put command construction and terminal concerns
  in `internal/cli`, interactive behavior in `internal/tui`, and protocol/HTTP
  behavior in `internal/scim`.
- The `internal/scim` name is appropriate for the protocol implementation; it
  must not become a separately distributed generic `scim` command.
- Keep reusable behavior out of `main`. Version metadata is injected into
  `main.version` at build time.
- Preserve provider-specific SCIM attributes when decoding resources. Generic
  resources should remain capable of round-tripping fields the program does not
  understand.
- The current Go module path, `pool-skimmer`, is provisional. When the public
  GitHub owner is selected, update `go.mod`, all imports, installation examples,
  and release configuration together. Never invent the owner or repository URL.

## Public CLI contract

- Running `pool-skimmer` without positional arguments launches the TUI only
  when stdin and stdout are terminals. Explicit subcommands must remain usable
  non-interactively.
- Preserve the top-level `users`, `groups`, `doctor`, `version`, and Cobra
  completion interfaces unless a breaking change is intentional and documented.
- Preserve table output for humans and JSON/JSONL where currently supported for
  scripts. Do not mix progress or diagnostics into machine-readable stdout.
- Preserve the SCIM list controls: filters, one-based `start-index`, page size,
  `--all`, attribute inclusion, and attribute exclusion.
- Preserve JSON input from inline values, `@FILE`, and stdin. PATCH helpers must
  continue to accept repeatable add, replace, and remove operations.
- Mutating commands must retain `--dry-run`. Dry runs show the intended method,
  path, and payload without requiring credentials or contacting the endpoint.
- Deletion requires interactive confirmation; non-interactive callers must pass
  `--yes`. Do not make destructive commands easier to trigger accidentally.
- Convenience mutations must verify changes by reading the resource back unless
  the caller explicitly uses `--no-verify` for an incompatible provider.

## Configuration and credentials

- The service root is configured with a named profile under
  `~/.pool-skimmer/config.json`, `--endpoint`, or `SCIM_ENDPOINT` and must contain
  the provider's `Users` and `Groups` resources. Preserve multi-endpoint TUI
  selection and `--profile` support.
- Accept an API key through exactly one source: `--api-key`,
  `--api-key-file`, `--prompt-api-key`, `SCIM_API_KEY`, or
  `SCIM_API_KEY_FILE`, or the selected profile's credential file. Explicit flags
  take precedence over environment values, which take precedence over profiles.
- Keep named endpoint settings in `~/.pool-skimmer/config.json` and API keys in
  separate files under `~/.pool-skimmer/credentials/`. Directories use mode
  `0700`; files use mode `0600`. Never write API keys into `config.json` or reuse
  a profile key when its endpoint has been overridden.
- Keep custom `SCIM_AUTH_HEADER` and `SCIM_AUTH_SCHEME` support. The defaults are
  `Authorization` and `Bearer`; an explicitly empty scheme sends the key without
  a prefix.
- Keep request timeout and safe-read retry configuration available through
  named profiles, `SCIM_TIMEOUT`, `SCIM_READ_RETRIES`, and their flags.
- Never print, log, serialize, bake into an image, or include API keys in errors,
  dry runs, fixtures, screenshots, or diagnostics. Prefer API-key files or the
  secure interactive prompt in documentation.
- Use only unmistakably synthetic credentials, endpoints, users, and groups in
  tests and examples. Never commit `.env` files or real directory output.
- Public-release work must make HTTPS mandatory for remote endpoints, with any
  insecure HTTP override explicit and restricted to loopback development use.
  Until that plan item lands, do not weaken existing URL, embedded-credential,
  or header-injection validation.

## SCIM client invariants

- Use SCIM's one-based pagination. `ListAll` must make forward progress, retain
  partial pages in order, and stop safely on empty or completed results.
- Retry only safe GET requests for transient transport failures, rate limits,
  and eligible server failures. Never automatically retry POST, PATCH, PUT, or
  DELETE because the provider may already have applied the write.
- Honor provider retry guidance where supported and keep sleeps cancellable
  through the request context.
- Send PATCH documents with the SCIM PatchOp schema and preserve correct JSON
  types for booleans, arrays, objects, and strings.
- Support `If-Match` on PATCH, PUT, DELETE, and convenience mutations wherever
  the protocol allows it. Keep ETag-based concurrency protection visible to
  callers.
- Bound response reads, validate resource types, URL-escape resource IDs, reject
  credential-bearing endpoint URLs, and prevent authentication-header injection.
- Return useful SCIM error details and provider request IDs when available while
  keeping authorization values and sensitive payloads redacted.
- Keep discovery support provider-neutral: `ServiceProviderConfig`, `Schemas`,
  and `ResourceTypes`. Capability discovery may inform the UI, but providers can
  return incomplete metadata and must be handled defensively.

## TUI contract

- Keep all SCIM I/O asynchronous so rendering, navigation, and cancellation
  remain responsive. Do not perform network work directly in an update or view
  function.
- Load users and groups concurrently at startup. Pressing `r` from the resource
  browser or detail screen refreshes both collections from the endpoint and
  preserves the active selection when possible.
- Within the group-members screen, `r` refreshes that group's membership. Do not
  conflate this focused refresh with the global users/groups refresh.
- Preserve local filtering, full-resource inspection, editable common fields,
  user activation, membership changes, and guarded deletion.
- Preserve keyboard accessibility, the `?` help view, responsive narrow/wide
  layouts, visible loading/error states, and usable ANSI display widths.
- Keep destructive actions behind an explicit confirmation screen. Keep writes
  followed by read-back verification and show partial verification failures
  honestly.
- Retain the previous successfully loaded collection if a refresh fails; do not
  replace useful data with an empty result caused by an error.
- The current TUI loads all resources. Large-directory paging/lazy loading and
  capability-aware action disabling are tracked as post-launch work in
  `public-repository-plan.md`.

## Docker-only build and validation

- Always run tests and builds through Docker using repository Makefile targets.
  Do not rely on or install a Go toolchain on the host for repository work.
- Use `make test` for Go tests and `go vet` in the Docker test stage.
- Use `make image VERSION=<version>` to build the runnable Linux container.
- Use `make dist VERSION=<version>` to build Linux and macOS binaries for amd64
  and arm64. Use this instead of the host-dependent `make build` and
  `make install` targets; those targets do not satisfy repository validation.
- Add or update tests for every changed command behavior, TUI state transition,
  and HTTP contract. Use local `httptest` servers and synthetic SCIM documents.
- Build the runtime image after dependency, Dockerfile, Compose, or release
  changes. Run the relevant binary or image command after a successful build
  when practical; compilation alone does not validate the entrypoint.
- Do not claim live-provider validation unless the exact command was run against
  the configured provider. Local HTTP tests prove client behavior only.
- When reporting validation, state the commands run, whether Docker caching was
  involved when material, and which checks were not run.

## Container and release constraints

- Keep the runtime image based on `scratch`, statically linked, unprivileged as
  UID/GID `65532`, and free of shells, package managers, source, and build tools.
- Keep CA certificates available for HTTPS endpoints.
- Preserve the Compose hardening: read-only filesystem, all Linux capabilities
  dropped, and `no-new-privileges`.
- Never copy runtime credentials into an image layer. Compose passes supported
  `SCIM_*` settings at runtime.
- Linux and macOS on amd64 and arm64 are the supported binary targets. Windows
  packaging is intentionally out of scope until the user changes that decision.
- Keep generated binaries under ignored `dist/`; do not commit build artifacts.
- Public releases should eventually provide versioned archives, SHA-256
  checksums, SBOMs, provenance, GitHub Releases, and a multi-architecture GHCR
  image as specified in `public-repository-plan.md`.

## Documentation ownership

- `README.md` is the user-facing source for configuration, installation,
  commands, TUI behavior, safety, and supported platforms. Keep every example
  aligned with implemented flags and output.
- `AGENTS.md` is the concise, enforceable engineering handbook. Update it when
  product boundaries, structure, safety invariants, supported platforms, or
  validation workflows change.
- `public-repository-plan.md` owns public-release tasks and acceptance criteria.
  Update checkboxes only after the named evidence exists; do not mark planned
  security or release work complete based on intent.
- Avoid duplicating long implementation plans here. Link to the owning document
  while retaining the invariant future work must preserve.

## Repository hygiene

- Preserve unrelated user work and keep changes narrowly scoped to the request.
- Do not commit `.DS_Store`, `.env*`, coverage files, test binaries, the root
  executable, `dist/`, provider exports, or TUI captures containing real data.
- Keep `go.mod` and `go.sum` consistent and committed. Review new direct and
  transitive dependencies for necessity, maintenance, vulnerabilities, and
  license compatibility.
- Before the first public push, inspect the staged tree and complete reachable
  history for credentials and identifying directory data. Follow every release
  gate in `public-repository-plan.md`.
