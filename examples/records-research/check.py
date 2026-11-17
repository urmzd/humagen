"""Checks a chronology against the records, without trusting the answer.

usage: python3 check.py DIR

Reads DIR/out/chronology.csv (date, event, source_file, quote, certainty).
Every row must name a record that exists in DIR, and its quote must be in
that record word for word (runs of spaces and line breaks count as one
space). A date must be YYYY-MM-DD or the word unknown.
"""
import csv
import os
import re
import sys

root = sys.argv[1]


def flat(text):
    return " ".join(text.split())


with open(os.path.join(root, "out", "chronology.csv"), newline="") as f:
    rows = list(csv.DictReader(f))
bad = 0
for i, r in enumerate(rows, 2):
    name = r["source_file"]
    path = os.path.join(root, name)
    why = ""
    if os.sep in name or name.startswith(".") or not os.path.isfile(path):
        why = "no such record"
    elif not r["quote"].strip() or flat(r["quote"]) not in flat(open(path).read()):
        why = "quote is not in the record"
    elif r["date"] != "unknown" and not re.fullmatch(r"\d{4}-\d{2}-\d{2}", r["date"]):
        why = "date is not YYYY-MM-DD or unknown"
    if why:
        bad += 1
        print(f"row {i}: {why}: {name!r} {r['quote'][:60]!r}")
used = {r["source_file"] for r in rows}
unused = sorted(n for n in os.listdir(root) if os.path.isfile(os.path.join(root, n)) and n not in used)
print(f"rows {len(rows)}, bad {bad}, unknown dates {sum(r['date'] == 'unknown' for r in rows)}, records never cited {unused}")
sys.exit(1 if bad or not rows else 0)
