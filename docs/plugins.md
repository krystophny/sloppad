# Retired Extension And Plugin Direction

> **Legal notice:** Tabura is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](/DISCLAIMER.md).

This document used to define Tabura's extension/plugin runtime and the split
between public core and private capability bundles.

That is no longer the active product direction.

## Current Direction

- No private `tabura-plugins` repo as a product dependency
- No extension/plugin bundle system as the preferred way to add behavior
- New product work belongs in the public `krystophny/tabura` repo under normal
  modular packages in `internal/`

## What This Means

- Core UI stays in core UI code
- Meeting-notes behavior stays in core meeting-notes code
- Privacy and safety invariants stay in core
- Any remaining `internal/extensions` and `internal/plugins` code should be
  treated as transitional compatibility code, not an expanding platform surface

## Planning References

- Architecture simplification tracker: `#128`
- Meeting-notes post-MVP public follow-up: `#129`, `#130`, `#131`, `#132`

## Historical Note

Old release notes and historical documents may still mention extension/plugin
runtime details because they recorded what existed at that time.
