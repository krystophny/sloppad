#!/usr/bin/env python3

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path

import soundfile as sf
import torch
from qwen_tts import Qwen3TTSModel


DEFAULT_MODEL_ID = os.environ.get("TABURA_HOTWORD_QWEN3TTS_MODEL", "Qwen/Qwen3-TTS-12Hz-1.7B-Base")
DEFAULT_LANGUAGE = os.environ.get("TABURA_HOTWORD_QWEN3TTS_LANGUAGE", "English")
TARGET_VARIANTS = (
    "Computer",
    "Computer.",
    "Computer!",
    "Computer?",
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate wake-word samples with Qwen3-TTS voice cloning.")
    parser.add_argument("--recordings-dir", required=True)
    parser.add_argument("--output-dir", required=True)
    parser.add_argument("--count", type=int, default=250)
    parser.add_argument("--model-id", default="qwen3tts")
    return parser.parse_args()


def load_recordings(recordings_dir: Path) -> list[dict]:
    recordings: list[dict] = []
    for meta_path in sorted(recordings_dir.glob("*.json")):
      try:
        payload = json.loads(meta_path.read_text())
      except Exception:
        continue
      file_name = str(payload.get("file_name", "")).strip()
      if not file_name:
        continue
      audio_path = recordings_dir / file_name
      if not audio_path.is_file():
        continue
      recordings.append({
        "id": str(payload.get("id", meta_path.stem)),
        "kind": str(payload.get("kind", "hotword")).strip().lower(),
        "path": audio_path,
      })
    return recordings


def build_model() -> Qwen3TTSModel:
    if torch.cuda.is_available():
        dtype = torch.bfloat16 if torch.cuda.is_bf16_supported() else torch.float16
        kwargs = {
            "device_map": "cuda:0",
            "dtype": dtype,
        }
        try:
            kwargs["attn_implementation"] = "flash_attention_2"
            return Qwen3TTSModel.from_pretrained(DEFAULT_MODEL_ID, **kwargs)
        except Exception:
            kwargs.pop("attn_implementation", None)
            return Qwen3TTSModel.from_pretrained(DEFAULT_MODEL_ID, **kwargs)
    return Qwen3TTSModel.from_pretrained(DEFAULT_MODEL_ID, device_map="cpu", dtype=torch.float32)


def build_prompts(model: Qwen3TTSModel, recordings: list[dict]) -> list[dict]:
    prompts: list[dict] = []
    hotword_first = sorted(recordings, key=lambda item: (item["kind"] != "hotword", item["id"]))
    for recording in hotword_first:
        path = str(recording["path"])
        kind = recording["kind"]
        ref_text = "Computer" if kind == "hotword" else None
        x_vector_only = kind != "hotword"
        prompt = model.create_voice_clone_prompt(
            ref_audio=path,
            ref_text=ref_text,
            x_vector_only_mode=x_vector_only,
        )
        prompts.append({
            "id": recording["id"],
            "kind": kind,
            "prompt": prompt,
        })
    return prompts


def main() -> int:
    args = parse_args()
    recordings_dir = Path(args.recordings_dir)
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    recordings = load_recordings(recordings_dir)
    if not recordings:
        raise SystemExit(f"no recordings found under {recordings_dir}")

    print(f"loaded {len(recordings)} prompt recording(s) from {recordings_dir}", flush=True)
    print(f"loading model {DEFAULT_MODEL_ID}", flush=True)
    model = build_model()
    print("model ready", flush=True)
    prompts = build_prompts(model, recordings)
    if not prompts:
        raise SystemExit("no usable prompt recordings found")
    print(f"prepared {len(prompts)} voice-clone prompt(s)", flush=True)

    count = max(1, int(args.count))
    manifest: list[dict] = []
    for index in range(count):
        prompt = prompts[index % len(prompts)]
        text = TARGET_VARIANTS[index % len(TARGET_VARIANTS)]
        wavs, sample_rate = model.generate_voice_clone(
            text=text,
            language=DEFAULT_LANGUAGE,
            voice_clone_prompt=prompt["prompt"],
        )
        if not wavs:
            raise SystemExit("Qwen3-TTS returned no audio")
        output_path = output_dir / f"{args.model_id}-{index + 1:04d}.wav"
        sf.write(output_path, wavs[0], sample_rate)
        manifest.append({
            "file": output_path.name,
            "prompt_id": prompt["id"],
            "prompt_kind": prompt["kind"],
            "text": text,
            "model": DEFAULT_MODEL_ID,
        })
        print(f"generated {index + 1}/{count}: {output_path.name}", flush=True)

    (output_dir / "manifest.json").write_text(json.dumps({
        "generator": "qwen3tts",
        "model": DEFAULT_MODEL_ID,
        "count": count,
        "language": DEFAULT_LANGUAGE,
        "items": manifest,
    }, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
