# Release v0.1.9

> **Legal notice:** Tabura is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](/DISCLAIMER.md).

## Scope

`v0.1.9` publishes the workspace-keyed chat model, the global inbox/context cleanup, and the current authoring/runtime surface built since `v0.1.8`.

## Highlights

### Workspace and Ontology Consolidation

- Chat sessions now key to workspaces instead of legacy project-only routing.
- Context and inbox behavior were tightened so the current ontology is reflected in storage, filtering, and UI labels.
- Intent execution was split into clearer layers, with canonical actions exposed directly instead of only through classifier mediation.

### Authoring, Capture, and Review Expansion

- Mail compose/reply/forward drafts landed across providers with interactive canvas behavior.
- Scan ingestion, print/scan position markers, and voice dictation composition expanded the document workflow surface.
- Artifact materialization, workspace archival, and artifact-kind taxonomy exposure broadened the review/runtime toolchain.

### Frontend and Runtime Hardening

- The frontend was ported to real TypeScript with type checking across the main browser modules.
- Bug-report routing, hotkeys, mobile/sidebar behavior, tap voice capture, and annotation/runtime edge cases were stabilized.
- The spec hub and README now point at the current release note for `v0.1.9`.

## Verification Scope

This release is intended to be verified against:

- `scripts/check-version-consistency.sh`
- `./scripts/sync-surface.sh --check`
- `go test ./...`
- `./scripts/playwright.sh`

## Traceability

For publication metadata, associate this release with:

- release label: `v0.1.9`
- repository: `https://github.com/krystophny/tabura`
- exact source revision: tag target commit hash
