# Contributing

Tabura is pre-stable and still young. Optimize for the best product and codebase,
not for speculative compatibility.

## Rewrite Policy

- Do not assume real external API consumers or compatibility obligations unless
  there is concrete evidence.
- Breaking changes, API removals, schema changes, renames, deletions, and UX
  rewrites are allowed and encouraged when they materially improve UX, code
  quality, or maintainability.
- Prefer rewriting or deleting stale code, stale docs, stale tests, stale
  endpoints, and stale issue assumptions over preserving them.
- Compatibility layers, migration shims, and deprecation periods require
  explicit justification. They are not the default.
- If an API, architecture, workflow, or scope premise is weak, replace it
  rather than preserving it for speculative compatibility.
- Historical docs and release notes may remain as records, but they must not
  steer new design if they conflict with the current direction.

## Primary Criteria

The only standing criteria for change are:

- excellence in UX
- code quality
- maintainability

## Practical Guidance

- Favor simpler public-core designs over compatibility baggage.
- Delete dead paths aggressively.
- Shrink or remove legacy integration surfaces unless they still earn their
  keep.
- Keep docs aligned with shipped reality, not with obsolete plans.
- If a cleanup is radical but clearly improves the product and codebase, do it.
