"""Writes made-up account exports to try the finance exports example on.

usage: python3 make-fixture.py DIR

Five exports of an invented household, a list of the four that are
authorized, and a copy of reconcile.py. Every name and number is
invented. The traps, on purpose:

- two checking exports overlap by two weeks, so their rows repeat;
- two coffees on one day for the same amount are two purchases, with
  two ids, and must both stay;
- the card statement's closing balance is 25.00 away from its rows;
- other-person-savings.csv is in the folder and not authorized.
"""
import os
import shutil
import sys

root = sys.argv[1]
os.makedirs(root, exist_ok=True)


def export(name, meta, rows):
    with open(os.path.join(root, name), "w") as f:
        for key, value in meta.items():
            f.write(f"# {key}: {value}\n")
        f.write("id,date,description,amount\n")
        for r in rows:
            f.write(",".join(r) + "\n")


jan = [("c001", "2026-01-02", "Salary Example Works", "3200.00"), ("c002", "2026-01-03", "Rent", "-1400.00"),
       ("c003", "2026-01-09", "Grocer Market", "-182.40"), ("c004", "2026-01-15", "Coffee Corner", "-4.50"),
       ("c005", "2026-01-15", "Coffee Corner", "-4.50"), ("c006", "2026-01-20", "Electric Company", "-96.10"),
       ("c007", "2026-01-28", "Card payment", "-300.00")]
feb_a = [("c008", "2026-02-02", "Salary Example Works", "3200.00"), ("c009", "2026-02-03", "Rent", "-1400.00"),
         ("c010", "2026-02-10", "Grocer Market", "-201.75"), ("c011", "2026-02-14", "Water Utility", "-38.20")]
feb_b = [("c012", "2026-02-20", "Electric Company", "-88.60"), ("c013", "2026-02-27", "Card payment", "-300.00"),
         ("c014", "2026-02-27", "Loan payment", "-250.00")]
mar = [("c015", "2026-03-02", "Salary Example Works", "3200.00"), ("c016", "2026-03-03", "Rent", "-1400.00"),
       ("c017", "2026-03-11", "Grocer Market", "-190.05"), ("c018", "2026-03-27", "Card payment", "-300.00"),
       ("c019", "2026-03-27", "Loan payment", "-250.00")]
export("checking-jan-to-mid-feb.csv", dict(account="checking", kind="bank", period_start="2026-01-01",
       period_end="2026-02-14", opening_balance="2500.00", closing_balance="5272.55"), jan + feb_a)
export("checking-feb-to-mar.csv", dict(account="checking", kind="bank", period_start="2026-02-01",
       period_end="2026-03-31", opening_balance="3712.50", closing_balance="5693.90"), feb_a + feb_b + mar)
export("card-jan-to-mar.csv", dict(account="card", kind="card", period_start="2026-01-01", period_end="2026-03-31",
       opening_balance="-2400.00", closing_balance="-1861.83", apr_percent="24.9", minimum_payment="65.00"),
       [("k001", "2026-01-12", "Grocer Market", "-64.20"), ("k002", "2026-01-28", "Card payment received", "300.00"),
        ("k003", "2026-01-31", "Interest", "-47.10"), ("k004", "2026-02-18", "Coffee Corner", "-12.80"),
        ("k005", "2026-02-27", "Card payment received", "300.00"), ("k006", "2026-02-28", "Interest", "-42.30"),
        ("k007", "2026-03-19", "Grocer Market", "-128.93"), ("k008", "2026-03-27", "Card payment received", "300.00"),
        ("k009", "2026-03-31", "Interest", "-41.50")])
export("loan-jan-to-mar.csv", dict(account="loan", kind="loan", period_start="2026-01-01", period_end="2026-03-31",
       opening_balance="-8200.00", closing_balance="-7816.40", apr_percent="6.5", minimum_payment="250.00"),
       [("l001", "2026-01-31", "Interest", "-44.40"), ("l002", "2026-02-27", "Loan payment received", "250.00"),
        ("l003", "2026-02-28", "Interest", "-43.60"), ("l004", "2026-03-27", "Loan payment received", "250.00"),
        ("l005", "2026-03-31", "Interest", "-28.40")])
export("other-person-savings.csv", dict(account="other-savings", kind="bank", period_start="2026-01-01",
       period_end="2026-03-31", opening_balance="9000.00", closing_balance="9000.00"), [])
with open(os.path.join(root, "authorized.txt"), "w") as f:
    f.write("# The exports this household has authorized. Nothing else in the folder is to be opened.\n"
            "checking-jan-to-mid-feb.csv\nchecking-feb-to-mar.csv\ncard-jan-to-mar.csv\nloan-jan-to-mar.csv\n")
shutil.copy(os.path.join(os.path.dirname(os.path.abspath(__file__)), "reconcile.py"), root)
print("wrote 5 exports, authorized.txt and reconcile.py under", root)
