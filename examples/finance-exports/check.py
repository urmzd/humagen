"""Recomputes the exports on its own and checks what was written under out/.

usage: python3 check.py DIR

It shares no code with reconcile.py. From authorized.txt and the exports
it rebuilds the set of transactions (one for each account and id), each
export's balance difference and the monthly and debt figures, and then
requires that:

- out/transactions.csv holds exactly those transactions, with their
  amounts, and none from a file that is not authorized;
- out/reconciliation.csv gives each export's difference;
- out/summary.csv gives the same figures;
- every row of out/plan.csv that carries an amount copies a row of
  out/summary.csv (same kind, name and cents), and every row of kind
  proposal carries none.
"""
import csv
import os
import sys
from fractions import Fraction

root = sys.argv[1]


def at(name):
    return os.path.join(root, name)


def table(name):
    with open(at(name), newline="") as f:
        return list(csv.DictReader(f))


def cents(text):
    value = Fraction(text) * 100
    assert value.denominator == 1, text
    return int(value)


allowed = [l.strip() for l in open(at("authorized.txt")) if l.strip() and not l.startswith("#")]
truth, diffs, debts = {}, {}, {}
for name in allowed:
    meta, total = {}, 0
    with open(at(name)) as f:
        lines = f.read().splitlines()
    for line in lines:
        if line.startswith("#") and ":" in line:
            k, v = line[1:].split(":", 1)
            meta[k.strip()] = v.strip()
    for r in csv.DictReader([l for l in lines if not l.startswith("#")]):
        total += cents(r["amount"])
        truth.setdefault((meta["account"], r["id"]), (cents(r["amount"]), r["date"]))
    diffs[name] = cents(meta["opening_balance"]) + total - cents(meta["closing_balance"])
    if meta.get("kind") in ("card", "loan"):
        debts[meta["account"]] = -cents(meta["closing_balance"])
faults = []
written = table("out/transactions.csv")
got = {(r["account"], r["id"]): int(r["cents"]) for r in written}
if len(got) != len(written):
    faults.append("a transaction is written twice")
if got != {k: v[0] for k, v in truth.items()}:
    faults.append(f"transactions differ: {len(got)} written, {len(truth)} expected")
strangers = sorted({r["source_file"] for r in written} - set(allowed))
if strangers:
    faults.append(f"rows from files that are not authorized: {strangers}")
recon = {r["file"]: int(r["difference_cents"]) for r in table("out/reconciliation.csv")}
if recon != diffs:
    faults.append(f"reconciliation differs: {recon} against {diffs}")
summary = {(r["kind"], r["name"]): int(r["cents"]) for r in table("out/summary.csv")}
months = {}
for r in written:
    key = ("month", f"{r['date'][:7]} {r['category']}")
    months[key] = months.get(key, 0) + truth.get((r["account"], r["id"]), (0,))[0]
for key, value in months.items():
    if summary.get(key) != value:
        faults.append(f"summary {key} is {summary.get(key)}, the rows give {value}")
for account, owed in debts.items():
    if summary.get(("debt", account)) != owed:
        faults.append(f"summary debt {account} is {summary.get(('debt', account))}, the export states {owed}")
plan = table("out/plan.csv") if os.path.isfile(at("out/plan.csv")) else None
copied = proposals = 0
if plan is None:
    faults.append("no out/plan.csv")
else:
    for i, r in enumerate(plan, 2):
        if r["kind"] == "proposal":
            proposals += 1
            if r["cents"].strip():
                faults.append(f"plan row {i}: a proposal carries an amount")
        elif not r["cents"].strip().lstrip("-").isdigit() or summary.get((r["kind"], r["name"])) != int(r["cents"]):
            faults.append(f"plan row {i}: {r['kind']} {r['name']!r} {r['cents']!r} is not a row of out/summary.csv")
        else:
            copied += 1
for fault in faults:
    print("FAULT:", fault)
print(f"transactions {len(written)}, exports off {sorted(n for n, d in diffs.items() if d)}, "
      f"plan rows copied {copied}, proposals {proposals}, faults {len(faults)}")
sys.exit(1 if faults else 0)
