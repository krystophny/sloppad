# Flow Definitions

`tests/flows/` is the source of truth for cross-platform UI interaction flows.

Each `.yaml` file defines one platform-agnostic flow:

```yaml
name: tabura_circle_select_ink_tool
description: Open the circle, select ink, and keep the circle open for more changes.
tags: [circle, tool]
preconditions:
  tool: pointer
  session: none
  silent: false
steps:
  - action: tap
    target: tabura_circle_dot
    expect:
      tabura_circle: expanded
  - action: tap
    target: tabura_circle_segment_ink
    expect:
      active_tool: ink
      tabura_circle: expanded
```

Supported schema:

- `name`: unique string
- `description`: human-readable string
- `tags`: non-empty string array
- `preconditions`:
  - `tool`: `pointer|highlight|ink|text_note|prompt`
  - `session`: `none|dialogue|meeting`
  - `silent`: boolean
  - `indicator_state`: `idle|listening|paused|recording|working`
- `steps[]`:
  - `action`: `tap|tap_outside|verify|wait`
  - `target`: logical target id for `tap` and optional for `verify`
  - `duration_ms`: required for `wait`
  - `expect`: logical assertions
  - `platforms`: optional subset of `web|ios|android`

Supported logical assertions:

- `active_tool`
- `session`
- `silent`
- `tabura_circle`
- `dot_inner_icon`
- `body_class_contains`
- `indicator_state`
- `cursor_class`

The Playwright adapter currently executes these flows against the browser harness in
`tests/playwright/flow-harness.html`. The same logical targets and assertions are
structured so native adapters can consume them once the iOS and Android clients land.
