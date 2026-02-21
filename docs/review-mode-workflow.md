# Review Mode Workflow

## Scope

Review mode is annotation-first and commit-driven.

It supports:
- selection capture
- draft mark creation
- optional comment attachment
- explicit commit to persistent annotations

## State Model

```text
Idle
  -> SelectionActive
  -> IntentCaptured
  -> DraftMarkSet
  -> PreviewReady
  -> Committed
```

Cancellation paths may return to `Idle` from any non-committed state.

## Event and Transition Rules

- Selection event enters `SelectionActive`.
- Comment/prompt submission enters `IntentCaptured`.
- Mark write event (`canvas_mark_set`) enters `DraftMarkSet`.
- UI preview enters `PreviewReady`.
- Commit event (`canvas_commit`) enters `Committed`.
- Cancel clears pending draft for target and returns to `Idle`.

## Mark Semantics

Draft mark characteristics:
- bound to a concrete artifact target
- editable while draft
- removable before commit

Commit behavior:
- includes draft marks when requested
- writes annotation artifacts for persistent review state

## Batch-Oriented Review

Batch workflow requirement:
1. filter draft marks by class/intention
2. produce ordered preview sequence
3. apply per-item decision (accept/reject/edit)
4. execute commit for accepted set

Current `v0.0.1` runtime includes draft and commit primitives; full batch orchestration is documented behavior target and may be completed incrementally in future versions.

## Failure Handling

- Empty or invalid selection: reject with no mutation.
- Invalid target reference: reject mark set request.
- Commit with no eligible drafts: no-op success or explicit empty-result status.
- UI cancellation: clear transient popover/capture state and restore focus.
