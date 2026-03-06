# Retired Extension-Platform Whitepaper

> **Legal notice:** Tabura is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](/DISCLAIMER.md).

This file previously documented a local-first extension platform for Tabura.

That is no longer the active product direction.

## Current Direction

- One public repo for product behavior: `krystophny/tabura`
- Modular internal packages instead of extension bundles
- No private premium bundle repo as part of the intended architecture

## Practical Implications

- UI work stays in core web code
- Meeting-notes logic stays in core meeting-notes code
- Planning and tracking stay in public GitHub issues
- Existing extension/plugin runtime code should be treated as legacy
  compatibility code pending consolidation/removal

## Public Tracking

- `#128` Simplify architecture: remove extension/plugin host and keep modular public core

## Historical Note

Release notes and older commits may still mention extension-platform work. That
material is historical and should not be used to steer new feature design.
