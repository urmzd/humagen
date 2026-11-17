"""Removes duplicates, reconciles balances and totals the authorized exports.

usage: python3 reconcile.py        (run in the folder that holds the exports)

Reads authorized.txt, one file name a line, and only those files. An
export is a CSV whose first lines are "# key: value" (account, kind as
bank, card or loan, period_start, period_end, opening_balance,
closing_balance, and for a debt apr_percent and minimum_payment), then
the header id,date,description,amount. Amounts are signed: money in is
positive. Writes, under out/:

  transactions.csv    each transaction once, with the file and row it is
                      from. Two rows are one transaction when they have
                      the same account and id; nothing else is merged.
  reconciliation.csv  for each export, opening balance plus its rows
                      against the closing balance it states.
  summary.csv         cents by month and category, and each debt's
                      balance, rate and minimum.

Money is whole cents throughout. Nothing here is rounded.
"""
import csv
import os
from decimal import Decimal

CATEGORIES = [("payment received", "payment received"), ("salary", "income"), ("rent", "housing"), ("grocer", "groceries"), ("coffee", "coffee"),
              ("electric", "utilities"), ("water", "utilities"), ("card payment", "debt payment"),
              ("loan payment", "debt payment"), ("interest", "interest and fees"), ("fee", "interest and fees")]


def cents(text):
    return int((Decimal(text) * 100).to_integral_exact())


def category(description):
    low = description.lower()
    for word, name in CATEGORIES:
        if word in low:
            return name
    return "other"


def load(name):
    meta, rows = {}, []
    with open(name, newline="") as f:
        lines = f.read().splitlines()
    body = [line for line in lines if not line.startswith("#")]
    for line in lines:
        if line.startswith("#") and ":" in line:
            key, value = line[1:].split(":", 1)
            meta[key.strip()] = value.strip()
    for n, r in enumerate(csv.DictReader(body), start=len(lines) - len(body) + 2):
        rows.append({"id": r["id"], "date": r["date"], "description": r["description"],
                     "cents": cents(r["amount"]), "file": name, "row": n})
    return meta, rows


def main():
    with open("authorized.txt") as f:
        names = [line.strip() for line in f if line.strip() and not line.startswith("#")]
    os.makedirs("out", exist_ok=True)
    seen, ledger, recon, debts = {}, [], [], []
    for name in names:
        meta, rows = load(name)
        total = sum(r["cents"] for r in rows)
        opening, closing = cents(meta["opening_balance"]), cents(meta["closing_balance"])
        recon.append([name, meta["account"], meta["period_start"], meta["period_end"], opening, total,
                      closing, opening + total, opening + total - closing])
        if meta.get("kind") in ("card", "loan"):
            debts.append((meta["period_end"], meta["account"], -closing, meta.get("apr_percent", ""),
                          cents(meta.get("minimum_payment", "0")), name))
        for r in rows:
            key = (meta["account"], r["id"])
            if key in seen:
                seen[key]["also_in"].append(f"{r['file']}:{r['row']}")
                continue
            r.update(account=meta["account"], category=category(r["description"]), also_in=[])
            seen[key] = r
            ledger.append(r)
    ledger.sort(key=lambda r: (r["date"], r["account"], r["id"]))
    with open("out/transactions.csv", "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["account", "id", "date", "description", "cents", "category", "source_file", "source_row", "also_in"])
        for r in ledger:
            w.writerow([r["account"], r["id"], r["date"], r["description"], r["cents"], r["category"],
                        r["file"], r["row"], " ".join(r["also_in"])])
    with open("out/reconciliation.csv", "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["file", "account", "period_start", "period_end", "opening_cents", "rows_cents",
                    "closing_stated_cents", "closing_computed_cents", "difference_cents"])
        w.writerows(recon)
    months = {}
    for r in ledger:
        key = (r["date"][:7], r["category"])
        months[key] = months.get(key, 0) + r["cents"]
    latest = {}
    for end, account, owed, apr, minimum, name in sorted(debts):
        latest[account] = (owed, apr, minimum, name, end)
    with open("out/summary.csv", "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["kind", "name", "cents", "detail"])
        for (month, cat), value in sorted(months.items()):
            w.writerow(["month", f"{month} {cat}", value, ""])
        for account, (owed, apr, minimum, name, end) in sorted(latest.items()):
            w.writerow(["debt", account, owed, f"apr_percent={apr} as of {end} from {name}"])
            w.writerow(["minimum", account, minimum, f"from {name}"])
    merged = sum(len(r["also_in"]) for r in ledger)
    off = [row[0] for row in recon if row[-1]]
    print(f"exports {len(names)}, transactions {len(ledger)}, duplicate rows removed {merged}, exports that do not reconcile {off}")


if __name__ == "__main__":
    main()
