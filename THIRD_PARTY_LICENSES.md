# Third-Party Licenses

This document tracks third-party components used by Tabura and whether they are
bundled in the repository build outputs or downloaded at setup/runtime.

## Bundled In Binary

| Component | License | Notes |
|---|---|---|
| marked.js | MIT | Embedded web asset |
| highlight.js | BSD-3-Clause | Embedded web asset |
| PDF.js | Apache-2.0 | Embedded web asset |
| MathJax | Apache-2.0 | Embedded web asset |
| Liberation fonts | SIL OFL 1.1 | Embedded font assets |
| Foxit PDF fonts | BSD | Embedded font assets |

## Downloaded At Setup Time

| Component | License | Delivery model |
|---|---|---|
| Piper TTS Python runtime | GPL | Installed into isolated Python virtualenv and exposed over local HTTP |
| Piper voice models | Per-model (see Hugging Face model cards) | Downloaded model files |
| ffmpeg | GPL/LGPL (package dependent) | Installed as system package |
| voxtype | MIT | Installed as external executable |

## Planned Optional Downloads

| Component | License | Notes |
|---|---|---|
| ONNX Runtime Web | MIT | Browser-side runtime for local inference |
| openWakeWord models | Apache-2.0 | Local wake-word inference models |
| DistilBERT intent model | Apache-2.0 | Local intent sidecar model |
| Qwen3 0.6B GGUF | Apache-2.0 | Optional local mid-tier intent fallback |
| llama.cpp | MIT | Optional local LLM server runtime |

## Licensing Boundary

- Tabura's Go binary remains MIT-licensed.
- GPL-governed Piper runtime is integrated as a loopback HTTP sidecar process and
  is not linked into the Go binary.
- Voice model licensing is handled per model card; setup scripts print a Tier-2
  notice before download.
