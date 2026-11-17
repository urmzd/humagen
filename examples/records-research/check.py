"""Checks a chronology against the records, without trusting the answer.

usage: python3 check.py DIR
       python3 check.py --self-test

Reads DIR/out/chronology.csv (date, event, source_file, quote, certainty).
Every row must name a record that exists in DIR, and its quote must be in
that record word for word (runs of spaces and line breaks count as one
space). The date must be one the quote itself states:

  YYYY-MM-DD  the quote states that day, month and year
  ????-MM-DD  the quote states that day and month, and no year
  unknown     the quote states no date

A date that is plausible and not in the quote fails: the day of a nearby
appointment, or the year of the records around it. The certainty must be
exactly stated, conflicting or unknown.
"""
import csv
import os
import re
import sys

MONTHS = {m: i + 1 for i, m in enumerate(
    ["january", "february", "march", "april", "may", "june", "july",
     "august", "september", "october", "november", "december"])}
MONTHS.update({m[:3]: i for m, i in list(MONTHS.items())})
NAME = "|".join(sorted(MONTHS, key=len, reverse=True))


def stated(quote):
    """The dates a quote states: full ones as (y, m, d), and day and month
    with no year as (None, m, d)."""
    q, full, part = quote.lower(), set(), set()
    for y, m, d in re.findall(r"\b(\d{4})-(\d{2})-(\d{2})\b", q):
        full.add((int(y), int(m), int(d)))
    for name, d, y in re.findall(rf"\b({NAME})\.? (\d{{1,2}})(?:st|nd|rd|th)?(?:,? (\d{{4}}))?", q):
        (full if y else part).add((int(y) if y else None, MONTHS[name], int(d)))
    for m, d, y in re.findall(r"(?<![\d/])(\d{1,2})/(\d{1,2})(?:/(\d{4}))?(?![\d/])", q):
        (full if y else part).add((int(y) if y else None, int(m), int(d)))
    part = {p for p in part if not any(f[1:] == p[1:] for f in full)}
    return full, part


def date_fault(date, quote):
    """Why a row's date is not what its quote states, or ""."""
    full, part = stated(quote)
    m = re.fullmatch(r"(\d{4}|\?\?\?\?)-(\d{2})-(\d{2})", date)
    if date == "unknown":
        return ""
    if not m:
        return "date is not YYYY-MM-DD, ????-MM-DD or unknown"
    month, day = int(m.group(2)), int(m.group(3))
    if m.group(1) == "????":
        return "" if (None, month, day) in part else "the quote does not state that day and month without a year"
    return "" if (int(m.group(1)), month, day) in full else "the quote does not state that date"


def certainty_fault(certainty):
    """Why a row's certainty is not one of the three words, or ""."""
    return "" if certainty in ("stated", "conflicting", "unknown") else "certainty is not stated, conflicting or unknown"


def flat(text):
    return " ".join(text.split())


def check(root):
    with open(os.path.join(root, "out", "chronology.csv"), newline="") as f:
        rows = list(csv.DictReader(f))
    bad = 0
    for i, r in enumerate(rows, 2):
        name = r["source_file"]
        path = os.path.join(root, name)
        if os.sep in name or name.startswith(".") or not os.path.isfile(path):
            why = "no such record"
        elif not r["quote"].strip() or flat(r["quote"]) not in flat(open(path).read()):
            why = "quote is not in the record"
        else:
            why = date_fault(r["date"], r["quote"]) or certainty_fault(r["certainty"])
        if why:
            bad += 1
            print(f"row {i}: {why}: {r['date']} {name!r} {r['quote'][:60]!r}")
    used = {r["source_file"] for r in rows}
    unused = sorted(n for n in os.listdir(root) if os.path.isfile(os.path.join(root, n)) and n not in used)
    unknown = sum(r["date"] == "unknown" for r in rows)
    partial = sum(r["date"].startswith("????") for r in rows)
    print(f"rows {len(rows)}, bad {bad}, unknown dates {unknown}, dates without a year {partial}, records never cited {unused}")
    return 1 if bad or not rows else 0


def self_test():
    cases = [
        # date, quote, passes
        ("2025-04-02", "Date and time: April 2, 2025 at 9:00 AM", True),
        ("2025-04-02", "biometrics done 4/3, took 20 minutes", False),   # the day of a nearby appointment
        ("2025-04-03", "biometrics done 4/3, took 20 minutes", False),   # the year of the records around it
        ("????-04-03", "biometrics done 4/3, took 20 minutes", True),
        ("2025-06-06", "We mailed the response on June 6.", False),
        ("????-06-06", "We mailed the response on June 6.", True),
        ("????-05-20", "the request for evidence dated May 20 asked for your tax", True),
        ("????-04-02", "Date and time: April 2, 2025 at 9:00 AM", False),  # the year is stated: use it
        ("2025-03-04", "Notice date: 3/4/2025", True),
        ("2025-03-04", "Notice date: 2025-03-04", True),
        ("2025-09-16", "interview went fine, officer said decision cannot be made yet", False),
        ("unknown", "interview went fine, officer said decision cannot be made yet", True),
        ("2025-13-40", "Notice date: March 4, 2025", False),
        ("spring 2025", "Notice date: March 4, 2025", False),
    ]
    wrong = 0
    for date, quote, passes in cases:
        fault = date_fault(date, quote)
        ok = (fault == "") == passes
        wrong += not ok
        print(("ok   " if ok else "WRONG"), date, repr(quote[:44]), "->", fault or "accepted")
    for certainty, passes in [("stated", True), ("conflicting", True), ("unknown", True),
                              ("stated (probably)", False), ("Stated", False), ("", False)]:
        fault = certainty_fault(certainty)
        ok = (fault == "") == passes
        wrong += not ok
        print(("ok   " if ok else "WRONG"), "certainty", repr(certainty), "->", fault or "accepted")
    print(f"{len(cases) + 6} cases, {wrong} wrong")
    return 1 if wrong else 0


if __name__ == "__main__":
    sys.exit(self_test() if sys.argv[1] == "--self-test" else check(sys.argv[1]))
