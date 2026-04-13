#!/usr/bin/env python3
"""
Print a coverage audit of data/lectures.json by source.

Usage:
    python scripts/audit.py
    python scripts/audit.py data/lectures-enriched.json
"""

import json
import sys
from collections import defaultdict

path = sys.argv[1] if len(sys.argv) > 1 else "data/lectures.json"

with open(path) as f:
    lectures = json.load(f)

by_host = defaultdict(list)
for lecture in lectures:
    by_host[lecture["host_slug"]].append(lecture)

print(f"\n{'Source':<28} {'Events':>6}  {'Summary':>7}  {'Speakers':>8}  {'Image':>5}")
print("-" * 62)
for host, lecs in sorted(by_host.items(), key=lambda x: -len(x[1])):
    n = len(lecs)
    has_summary = sum(1 for lecture in lecs if lecture.get("summary", "").strip())
    has_speakers = sum(1 for lecture in lecs if lecture.get("speakers"))
    has_image = sum(1 for lecture in lecs if lecture.get("image", "").strip())
    print(f"{host:<28} {n:>6}  {has_summary:>7}  {has_speakers:>8}  {has_image:>5}")

print("-" * 62)
print(
    f"{'TOTAL':<28} {len(lectures):>6}  "
    f"{sum(1 for lecture in lectures if lecture.get('summary', '').strip()):>7}  "
    f"{sum(1 for lecture in lectures if lecture.get('speakers')):>8}  "
    f"{sum(1 for lecture in lectures if lecture.get('image', '').strip()):>5}"
)
print()

# Flag likely non-lectures
print("Possible non-lecture events (check these):")
noise_keywords = [
    "concert",
    "festival",
    "workshop",
    "open day",
    "school holiday",
    "tour",
    "performance",
    "exhibition",
    "live ",
    "farming",
    "printopia",
]
for lecture in lectures:
    text = (lecture.get("title", "") + " " + lecture.get("summary", "")).lower()
    for kw in noise_keywords:
        if kw in text:
            print(f"  [{lecture['host_slug']}] {lecture['title'][:70]}")
            break
