"""Recomputes the exports on its own and checks what was written under out/.

usage: python3 check.py DIR

It shares no code with reconcile.py: the rules are written out a second
time here, including the words that give a category. From authorized.txt
and the exports it rebuilds every transaction with its date, description,
amount, category, the file and line it is first on and the lines of its
exact copies, each export's balance difference, and the monthly and debt
figures. It fails when:

- an amount in an export is not a finite number of whole cents;
- two rows with one account and id disagree on date, description or
  amount (reconcile.py must have stopped, not merged them);
- out/transactions.csv is not exactly those transactions, field for
  field, or draws on a file that is not authorized;
- out/reconciliation.csv or out/summary.csv differ from the figures;
- a row of out/plan.csv that carries an amount is not the row of
  out/summary.csv its source names (out/summary.csv:N, the header being
  line 1), or a row of kind proposal carries an amount.
"""
import csv
import os
import re
import sys
from fractions import Fraction

WORDS = [("payment received", "payment received"), ("salary", "income"), ("rent", "housing"),
         ("grocer", "groceries"), ("coffee", "coffee"), ("electric", "utilities"), ("water", "utilities"),
         ("card payment", "debt payment"), ("loan payment", "debt payment"),
         ("interest", "interest and fees"), ("fee", "interest and fees")]

root = sys.argv[1]
faults = []


def at(name):
    return os.path.join(root, name)


def table(name):
    with open(at(name), newline="") as f:
        return list(csv.DictReader(f))


def money(text, where):
    if not re.fullmatch(r"\s*-?\d+(\.\d{1,2})?\s*", text):
        faults.append(f"{where}: {text!r} is not a finite number of whole cents")
        return 0
    return int(Fraction(text.strip()) * 100)


def kind_of(description):
    low = description.lower()
    return next((name for word, name in WORDS if word in low), "other")


allowed = [l.strip() for l in open(at("authorized.txt")) if l.strip() and not l.startswith("#")]
truth, order, diffs, debts, clashes = {}, [], {}, {}, []
for name in allowed:
    meta, total = {}, 0
    with open(at(name)) as f:
        lines = f.read().splitlines()
    for n, line in enumerate(lines, 1):
        if line.startswith("#"):
            if ":" in line:
                k, v = line[1:].split(":", 1)
                meta[k.strip()] = v.strip()
            continue
        if line.startswith("id,"):
            continue
        fields = next(csv.reader([line]))
        ident, date, description, amount = fields[0], fields[1], fields[2], fields[3]
        value = money(amount, f"{name}:{n}")
        total += value
        key = (meta["account"], ident)
        if key not in truth:
            truth[key] = dict(date=date, description=description, cents=value, category=kind_of(description),
                              source_file=name, source_row=n, also_in=[])
            order.append(key)
        elif (truth[key]["date"], truth[key]["description"], truth[key]["cents"]) != (date, description, value):
            clashes.append(f"{key[0]} {key[1]} at {truth[key]['source_file']}:{truth[key]['source_row']} and {name}:{n}")
        else:
            truth[key]["also_in"].append(f"{name}:{n}")
    diffs[name] = (money(meta["opening_balance"], f"{name} opening_balance") + total
                   - money(meta["closing_balance"], f"{name} closing_balance"))
    if meta.get("kind") in ("card", "loan"):
        debts[meta["account"]] = -money(meta["closing_balance"], f"{name} closing_balance")
for clash in clashes:
    faults.append(f"copies of one transaction disagree: {clash}")
if clashes and os.path.isfile(at("out/transactions.csv")):
    faults.append("out/transactions.csv was written although copies disagree")
written = table("out/transactions.csv") if os.path.isfile(at("out/transactions.csv")) else []
if not written:
    faults.append("no out/transactions.csv")
keys = [(r["account"], r["id"]) for r in written]
if len(set(keys)) != len(keys):
    faults.append("a transaction is written twice")
if set(keys) != set(truth) and not clashes:
    faults.append(f"transactions differ: {len(set(keys))} written, {len(truth)} expected")
for r in written:
    want = truth.get((r["account"], r["id"]))
    if want is None:
        continue
    have = dict(date=r["date"], description=r["description"], cents=r["cents"], category=r["category"],
                source_file=r["source_file"], source_row=r["source_row"], also_in=r["also_in"])
    expect = {k: (" ".join(v) if k == "also_in" else str(v)) for k, v in want.items()}
    for field in expect:
        if have[field] != expect[field]:
            faults.append(f"transaction {r['account']} {r['id']}: {field} is {have[field]!r}, the export gives {expect[field]!r}")
strangers = sorted({r["source_file"] for r in written} - set(allowed))
if strangers:
    faults.append(f"rows from files that are not authorized: {strangers}")
if written:
    recon = {r["file"]: int(r["difference_cents"]) for r in table("out/reconciliation.csv")}
    if recon != diffs:
        faults.append(f"reconciliation differs: {recon} against {diffs}")
    rows = table("out/summary.csv")
    summary = {(r["kind"], r["name"]): int(r["cents"]) for r in rows}
    months = {}
    for want in truth.values():
        key = ("month", f"{want['date'][:7]} {want['category']}")
        months[key] = months.get(key, 0) + want["cents"]
    figures = dict(months)
    figures.update({("debt", account): owed for account, owed in debts.items()})
    for key, value in figures.items():
        if summary.get(key) != value:
            faults.append(f"summary {key} is {summary.get(key)}, the exports give {value}")
    extra = sorted(k for k in summary if k not in figures and k[0] != "minimum")
    if extra:
        faults.append(f"summary rows the exports do not give: {extra}")
    copied = proposals = 0
    if not os.path.isfile(at("out/plan.csv")):
        faults.append("no out/plan.csv")
    else:
        for i, r in enumerate(table("out/plan.csv"), 2):
            if r["kind"] == "proposal":
                proposals += 1
                if r["cents"].strip():
                    faults.append(f"plan row {i}: a proposal carries an amount")
                continue
            m = re.fullmatch(r"out/summary\.csv:(\d+)", r["source"].strip())
            line = int(m.group(1)) if m else 0
            named = rows[line - 2] if 2 <= line <= len(rows) + 1 else None
            if named is None:
                faults.append(f"plan row {i}: source {r['source']!r} is not a line of out/summary.csv")
            elif (named["kind"], named["name"], named["cents"]) != (r["kind"], r["name"], r["cents"].strip()):
                faults.append(f"plan row {i}: {r['kind']} {r['name']!r} {r['cents']!r} is not line {line} of out/summary.csv")
            else:
                copied += 1
else:
    copied = proposals = 0
for fault in faults:
    print("FAULT:", fault)
print(f"transactions {len(written)}, exports off {sorted(n for n, d in diffs.items() if d)}, "
      f"plan rows copied {copied}, proposals {proposals}, faults {len(faults)}")
sys.exit(1 if faults else 0)
