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
    OLLAMA_HOST      Ollama base URL (default: http://localhost:11434)
    OLLAMA_MODEL     Model to use    (default: qwen2.5:14b)
    DRY_RUN          Set to 1 to print prompts without calling Ollama
    FORCE_REFRESH    Set to 1 to re-enrich all lectures, ignoring the cache
    REFRESH_SOURCE   host_slug to re-enrich (e.g. REFRESH_SOURCE=meetup), others stay cached
"""

import json
import os
import re as _re
import sys
import urllib.request

OLLAMA_HOST = os.environ.get("OLLAMA_HOST", "")
OLLAMA_MODEL = os.environ.get("OLLAMA_MODEL", "qwen2.5:14b")
DRY_RUN = os.environ.get("DRY_RUN", "0") == "1"
FORCE_REFRESH = os.environ.get("FORCE_REFRESH", "0") == "1"
REFRESH_SOURCE = os.environ.get(
    "REFRESH_SOURCE", ""
)  # re-enrich events from this host_slug
CACHE_ONLY = not OLLAMA_HOST  # apply cache without calling Ollama
INPUT = "data/lectures.json"
OUTPUT = "data/lectures-enriched.json"
CACHE = "data/enriched-cache.json"


def ollama_chat(prompt: str) -> str:
    payload = json.dumps(
        {
            "model": OLLAMA_MODEL,
            "prompt": prompt,
            "stream": False,
            # Disable thinking tokens on reasoning models (qwen3, deepseek-r1).
            # This keeps output clean JSON rather than <think>...</think> + JSON.
            "think": False,
        }
    ).encode()
    req = urllib.request.Request(
        f"{OLLAMA_HOST}/api/generate",
        data=payload,
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=120) as resp:
        return json.loads(resp.read())["response"].strip()


def _extract_json(raw: str) -> str:
    """Extract the first JSON object from a model response.

    Handles common wrapping patterns from reasoning models:
    - <think>...</think> blocks (qwen3, deepseek-r1)
    - ```json ... ``` or ``` ... ``` fences
    - Plain JSON object
    """
    import re

    # Strip <think>...</think> blocks produced by reasoning models.
    raw = re.sub(r"<think>.*?</think>", "", raw, flags=re.DOTALL).strip()

    # Strip markdown code fences.
    if "```" in raw:
        # Take content between first and last fence pair.
        parts = raw.split("```")
        # parts[1] is the fenced block (possibly prefixed with "json\n")
        if len(parts) >= 3:
            raw = parts[1]
            if raw.startswith("json"):
                raw = raw[4:]
            raw = raw.strip()

    # Find the first { and last } to extract a JSON object even if there's
    # trailing text after the closing brace.
    start = raw.find("{")
    end = raw.rfind("}")
    if start != -1 and end != -1 and end > start:
        raw = raw[start : end + 1]

    return raw.strip()


# HTML entity pattern (e.g. &mdash; &amp; &#8212;) and pure-punctuation names.
_JUNK_SPEAKER_RE = _re.compile(r"^(&[a-z#0-9]+;|[—–\-\s\.,:;!?]+)$", _re.IGNORECASE)


def _clean_speakers(speakers: list) -> list:
    """Drop speakers whose name is a HTML entity, punctuation, or empty."""
    cleaned = []
    for sp in speakers:
        name = sp.get("name", "").strip()
        if name and not _JUNK_SPEAKER_RE.match(name):
            cleaned.append(sp)
    return cleaned


def enrich(lecture: dict) -> dict:
    title = lecture.get("title", "")
    description = lecture.get("description", "") or lecture.get("summary", "")
    host = lecture.get("host_slug", "")

    is_thin = len(description.strip()) < 150

    prompt = f"""You are a curator for lectures.nz, a New Zealand public lectures website.

Given the event below, return ONLY a valid JSON object — no markdown, no explanation.

Fields:
- "event_type": One word classifying the event. Choose exactly one: lecture, seminar, panel, workshop, concert, market, ceremony, fitness, orientation, symposium, conference, other.
- "summary": One clear sentence (max 180 chars) for the index card. Capture the core topic and speaker if named. No hollow openers like "Join us" or "Discover". Do not invent anything not in the source.
- "description": 2-4 sentences for the detail page. Preserve the source's voice, key facts, people, and institutions. Remove hollow openers. Fix punctuation. {"Expand this — the source text is very short, so infer reasonable context from the title and host, but do not invent specific claims." if is_thin else "Preserve the existing text closely — only clean up punctuation and remove hollow openers."}
- "speakers": Array of speaker objects, each with "name" (string) and "bio" (string). Extract from title or description only. Return [] if none named. The "name" field must contain ONLY the person's name and honorific/title if given (e.g. "Dr Jane Smith" or "Professor John Doe") — never append event context, parenthetical notes, or topic references to the name. The "bio" field is a short role or affiliation, max 6 words (e.g. "former NZ diplomat", "Victoria University economist", "award-winning novelist"). Do not write a full sentence.

Event:
  host: {host}
  title: {title}
  description: {description}
"""

    if DRY_RUN:
        print(f"\n--- DRY RUN: {title[:60]} ---")
        print(prompt[:300])
        return lecture

    try:
        raw = ollama_chat(prompt)
        raw = _extract_json(raw)
        enriched = json.loads(raw)
        out = dict(lecture)
        if enriched.get("event_type"):
            out["event_type"] = enriched["event_type"]
        if enriched.get("summary"):
            out["summary"] = enriched["summary"]
        if enriched.get("description"):
            out["description"] = enriched["description"]
        if enriched.get("speakers"):
            out["speakers"] = _clean_speakers(enriched["speakers"])
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

    # Load cache: maps lecture ID → enriched fields from a previous run.
    cache = {}
    if os.path.exists(CACHE):
        with open(CACHE) as f:
            cache = json.load(f)

    skipped = sum(1 for l in lectures if l.get("id") in cache)
    todo = len(lectures) - skipped
    if CACHE_ONLY:
        print(
            f"Applying cache to {len(lectures)} lectures ({skipped} cached, {todo} unenriched)"
        )
    elif FORCE_REFRESH:
        print(
            f"FORCE_REFRESH: re-enriching all {len(lectures)} lectures using {OLLAMA_MODEL} @ {OLLAMA_HOST}"
        )
    elif REFRESH_SOURCE:
        source_count = sum(
            1 for l in lectures if l.get("host_slug", "") == REFRESH_SOURCE
        )
        print(
            f"REFRESH_SOURCE={REFRESH_SOURCE}: re-enriching {source_count} events, {skipped - source_count} others cached, using {OLLAMA_MODEL} @ {OLLAMA_HOST}"
        )
    else:
        print(
            f"Enriching {todo} lectures ({skipped} cached) using {OLLAMA_MODEL} @ {OLLAMA_HOST}"
        )

    enriched = []
    # Per-source stats: {slug: {"cached": int, "refreshed": int, "unenriched": int}}
    source_stats = {}

    for i, lec in enumerate(lectures, 1):
        lid = lec.get("id", "")
        slug = lec.get("host_slug", "unknown")
        title = lec.get("title", "")[:50]
        stats = source_stats.setdefault(
            slug, {"cached": 0, "refreshed": 0, "unenriched": 0}
        )

        is_source_refresh = (
            REFRESH_SOURCE and lec.get("host_slug", "") == REFRESH_SOURCE
        )
        if lid and lid in cache and not FORCE_REFRESH and not is_source_refresh:
            print(f"[{i:3d}/{len(lectures)}] {title} (cached)")
            out = dict(lec)
            cached_fields = cache[lid]
            # Don't wipe collect-time speakers with an empty cached value.
            if not cached_fields.get("speakers") and lec.get("speakers"):
                cached_fields = {k: v for k, v in cached_fields.items() if k != "speakers"}
            out.update(cached_fields)
            enriched.append(out)
            stats["cached"] += 1
            continue

        if CACHE_ONLY:
            enriched.append(lec)
            stats["unenriched"] += 1
            continue

        print(f"[{i:3d}/{len(lectures)}] {title}", end="", flush=True)
        result = enrich(lec)
        enriched.append(result)
        print(" ✓")
        stats["refreshed"] += 1

        # Cache the enriched fields keyed by ID.
        if lid:
            cache[lid] = {
                k: result[k]
                for k in ("event_type", "summary", "description", "speakers")
                if k in result
            }

    # Persist updated cache.
    with open(CACHE, "w") as f:
        json.dump(cache, f, indent=2, default=str)

    with open(OUTPUT, "w") as f:
        json.dump(enriched, f, indent=2, default=str)

    print(f"\nWrote {OUTPUT}")

    # Per-source summary.
    print(f"\n{'Source':<30} {'Cached':>8} {'Refreshed':>10} {'Unenriched':>12}")
    print("-" * 62)
    for slug in sorted(source_stats):
        s = source_stats[slug]
        print(f"{slug:<30} {s['cached']:>8} {s['refreshed']:>10} {s['unenriched']:>12}")
    print()
    print(f"Review with: diff <(jq '.[].title' {INPUT}) <(jq '.[].title' {OUTPUT})")


if __name__ == "__main__":
    main()
