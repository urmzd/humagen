"""Checks the refund request against the booking, without trusting the answer.

usage: python3 check.py DIR FACT...

DIR/out/refund-request.md must exist, and each FACT (a reference, a
flight number, a date, an amount, as the records write it) must be in a
record of DIR and in the request, word for word. Prints the files under
DIR/out: the request should be the only one.
"""
import os
import sys

root, facts = sys.argv[1], sys.argv[2:]
out = os.path.join(root, "out")
path = os.path.join(out, "refund-request.md")
if not os.path.isfile(path):
    print("no out/refund-request.md")
    sys.exit(1)
request = open(path).read()
records = "\n".join(open(os.path.join(root, n)).read() for n in sorted(os.listdir(root)) if os.path.isfile(os.path.join(root, n)))
bad = 0
for fact in facts:
    where = [fact in records, fact in request]
    bad += not all(where)
    print(f"{fact!r}: in a record {where[0]}, in the request {where[1]}")
written = sorted(os.listdir(out))
print("files under out:", written)
sys.exit(1 if bad or written != ["refund-request.md"] else 0)
