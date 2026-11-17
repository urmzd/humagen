"""Runs the script and the checker against cases that must pass or fail.

usage: python3 regress.py

Builds the generated exports in a temporary folder, runs reconcile.py,
writes a plan that copies the summary, and then changes one thing at a
time: an output, a plan source, or an input. Each case says whether
reconcile.py must stop and whether check.py must fail.
"""
import csv
import os
import shutil
import subprocess
import sys
import tempfile

here = os.path.dirname(os.path.abspath(__file__))


def run(script, folder, *args):
    cwd = folder if script == "reconcile.py" else here
    target = [] if script == "reconcile.py" else [folder]
    return subprocess.run([sys.executable, os.path.join(here, script), *target, *args],
                          cwd=cwd, capture_output=True, text=True).returncode


def rewrite(path, change):
    with open(path, newline="") as f:
        rows = list(csv.DictReader(f))
    fields = list(rows[0])
    change(rows)
    with open(path, "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader()
        w.writerows(rows)


def plan(folder):
    with open(os.path.join(folder, "out", "summary.csv"), newline="") as f:
        rows = list(csv.DictReader(f))
    with open(os.path.join(folder, "out", "plan.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["kind", "name", "cents", "source"])
        for n, r in enumerate(rows, 2):
            w.writerow([r["kind"], r["name"], r["cents"], f"out/summary.csv:{n}"])
        w.writerow(["proposal", "pay the debt with the higher rate first", "", ""])


def edit(folder, name, old, new):
    path = os.path.join(folder, name)
    text = open(path).read()
    assert old in text, (name, old)
    open(path, "w").write(text.replace(old, new))


def out(name):
    return lambda folder: os.path.join(folder, "out", name)


def set_field(file, row, field, value):
    return lambda folder: rewrite(out(file)(folder), lambda rows: rows[row].__setitem__(field, value))


CASES = [
    # name, change to outputs (after the script ran), change to inputs (before), script stops, checker fails
    ("the generated exports, untouched", None, None, False, False),
    ("a transaction's amount changed", set_field("transactions.csv", 3, "cents", "-18340"), None, False, True),
    ("a transaction's date moved within its month", set_field("transactions.csv", 3, "date", "2026-01-10"), None, False, True),
    ("a transaction's source row is 999999", set_field("transactions.csv", 3, "source_row", "999999"), None, False, True),
    ("a transaction's description changed", set_field("transactions.csv", 3, "description", "Grocer"), None, False, True),
    ("a transaction's category changed", set_field("transactions.csv", 3, "category", "other"), None, False, True),
    ("a copy's line dropped from also_in",
     lambda f: rewrite(out("transactions.csv")(f), lambda rows: next(r for r in rows if r["also_in"]).__setitem__("also_in", "")),
     None, False, True),
    ("a row attributed to the unauthorized export", set_field("transactions.csv", 0, "source_file", "other-person-savings.csv"), None, False, True),
    ("a transaction written twice", lambda f: rewrite(out("transactions.csv")(f), lambda rows: rows.append(dict(rows[8]))), None, False, True),
    ("the card's difference set to zero", lambda f: rewrite(out("reconciliation.csv")(f), lambda rows: [r.__setitem__("difference_cents", "0") for r in rows]), None, False, True),
    ("a plan amount off by one cent", set_field("plan.csv", 0, "cents", "-899"), None, False, True),
    ("a plan source naming line 999999", set_field("plan.csv", 0, "source", "out/summary.csv:999999"), None, False, True),
    ("a plan source naming another real line", set_field("plan.csv", 0, "source", "out/summary.csv:5"), None, False, True),
    ("a plan source in words, not a line", set_field("plan.csv", 0, "source", "out/summary.csv row 2"), None, False, True),
    ("a proposal given an amount", set_field("plan.csv", -1, "cents", "5000"), None, False, True),
    ("copies of one transaction that disagree on the amount, balances adjusted to still reconcile", None,
     lambda f: (edit(f, "checking-feb-to-mar.csv", "c008,2026-02-02,Salary Example Works,3200.00", "c008,2026-02-02,Salary Example Works,3201.00"),
                edit(f, "checking-feb-to-mar.csv", "closing_balance: 5693.90", "closing_balance: 5694.90")), True, True),
    ("copies that disagree on the date", None,
     lambda f: edit(f, "checking-feb-to-mar.csv", "c009,2026-02-03,Rent", "c009,2026-02-04,Rent"), True, True),
    ("an amount of 1.005", None, lambda f: edit(f, "card-jan-to-mar.csv", "k004,2026-02-18,Coffee Corner,-12.80", "k004,2026-02-18,Coffee Corner,-12.805"), True, True),
    ("an amount of NaN", None, lambda f: edit(f, "card-jan-to-mar.csv", "k004,2026-02-18,Coffee Corner,-12.80", "k004,2026-02-18,Coffee Corner,NaN"), True, True),
    ("an amount of 1e2", None, lambda f: edit(f, "card-jan-to-mar.csv", "k004,2026-02-18,Coffee Corner,-12.80", "k004,2026-02-18,Coffee Corner,1e2"), True, True),
]

wrong = 0
for name, change_out, change_in, stops, fails in CASES:
    folder = tempfile.mkdtemp()
    try:
        subprocess.run([sys.executable, os.path.join(here, "make-fixture.py"), folder], capture_output=True, check=True)
        if change_in:
            change_in(folder)
        stopped = run("reconcile.py", folder) != 0
        if not stopped:
            plan(folder)
            if change_out:
                change_out(folder)
        failed = run("check.py", folder) != 0
        ok = (stopped, failed) == (stops, fails)
        wrong += not ok
        print(("ok   " if ok else "WRONG"), name, "-> script", "stopped" if stopped else "ran", "| checker", "failed" if failed else "passed")
    finally:
        shutil.rmtree(folder)
print(f"{len(CASES)} cases, {wrong} wrong")
sys.exit(1 if wrong else 0)
