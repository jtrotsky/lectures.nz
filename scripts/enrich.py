#!/usr/bin/env python3
"""
Enrich lectures.json using a local Ollama instance.

For each lecture, attempts to:
  1. Extract speaker name(s) from title + summary text
  2. Rewrite the title to be cleaner and more compelling
  3. Expand a thin summary into a richer 2-3 sentence description

Writes output to data/lectures-enriched.json. Does NOT overwrite the
original — a human should review the diff before committing.

Usage:
    python scripts/enrich.py

Environment variables:
    OLLAMA_HOST   Ollama base URL (default: http://localhost:11434)
    OLLAMA_MODEL  Model to use    (default: llama3)
    DRY_RUN       Set to 1 to print prompts without calling Ollama
"""

import json
import os
import sys
import urllib.request
import urllib.error

OLLAMA_HOST = os.environ.get("OLLAMA_HOST", "http://localhost:11434")
OLLAMA_MODEL = os.environ.get("OLLAMA_MODEL", "llama3")
DRY_RUN = os.environ.get("DRY_RUN", "0") == "1"
INPUT = "data/lectures.json"
OUTPUT = "data/lectures-enriched.json"


def ollama_chat(prompt: str) -> str:
    payload = json.dumps({
        "model": OLLAMA_MODEL,
        "prompt": prompt,
        "stream": False,
    }).encode()
    req = urllib.request.Request(
        f"{OLLAMA_HOST}/api/generate",
        data=payload,
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=60) as resp:
        return json.loads(resp.read())["response"].strip()


def enrich(lecture: dict) -> dict:
    title = lecture.get("title", "")
    summary = lecture.get("summary", "")
    host = lecture.get("host_slug", "")

    prompt = f"""You are helping curate a New Zealand public lectures website.

Given the following event details, return a JSON object with these fields:
- "title": a clear, compelling title (fix ALL-CAPS, clean up punctuation, keep it concise)
- "summary": 2-3 sentence description suitable for a public audience. Use the existing text as a base — expand if thin, tighten if verbose. Do not invent facts.
- "speakers": a JSON array of objects with "name" (string) and "bio" (string, 1 sentence, empty string if unknown). Extract from the title/summary if present, otherwise return [].

Respond with ONLY valid JSON, no explanation.

Event:
  host: {host}
  title: {title}
  summary: {summary}
"""

    if DRY_RUN:
        print(f"\n--- DRY RUN: {title[:60]} ---")
        print(prompt[:300])
        return lecture

    try:
        raw = ollama_chat(prompt)
        # Strip markdown code fences if present
        if raw.startswith("```"):
            raw = raw.split("```")[1]
            if raw.startswith("json"):
                raw = raw[4:]
        enriched = json.loads(raw)
        out = dict(lecture)
        if enriched.get("title"):
            out["title"] = enriched["title"]
        if enriched.get("summary"):
            out["summary"] = enriched["summary"]
        if enriched.get("speakers"):
            out["speakers"] = enriched["speakers"]
        return out
    except Exception as e:
        print(f"  WARN: {title[:50]}: {e}", file=sys.stderr)
        return lecture


def main():
    if not os.path.exists(INPUT):
        print(f"ERROR: {INPUT} not found. Run: go run ./cmd/collect", file=sys.stderr)
        sys.exit(1)

    with open(INPUT) as f:
        lectures = json.load(f)

    print(f"Enriching {len(lectures)} lectures using {OLLAMA_MODEL} @ {OLLAMA_HOST}")

    enriched = []
    for i, lec in enumerate(lectures, 1):
        title = lec.get("title", "")[:50]
        print(f"[{i:3d}/{len(lectures)}] {title}", end="", flush=True)
        result = enrich(lec)
        enriched.append(result)
        print(" ✓")

    with open(OUTPUT, "w") as f:
        json.dump(enriched, f, indent=2, default=str)

    print(f"\nWrote {OUTPUT}")
    print(f"Review with: diff <(jq '.[].title' {INPUT}) <(jq '.[].title' {OUTPUT})")


if __name__ == "__main__":
    main()
