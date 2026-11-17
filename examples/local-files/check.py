"""Checks what the filer did against the folder, without trusting its answer.

usage:
  python3 check.py snapshot DIR > before.sha    before anything runs
  python3 check.py plan DIR                     after the survey
  python3 check.py after DIR before.sha KIND... after the approved moves

plan: every row of DIR/plan.csv names a file that exists with that size
and sha256, every file is in the plan, and proposed paths keep the name.
after: every content hash of the snapshot is still in the folder, rows
of the approved kinds sit at their proposed paths, and no other row moved.
"""
import csv
import hashlib
import os
import sys

OURS = {"plan.csv", "changes.log"}


def sha(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def files(root):
    for d, _, names in os.walk(root):
        for n in names:
            if n not in OURS:
                yield os.path.join(d, n)


def rows(root):
    with open(os.path.join(root, "plan.csv"), newline="") as f:
        return list(csv.DictReader(f))


def at(root, rel):
    return os.path.normpath(os.path.join(root, rel.lstrip("./")))


mode, root = sys.argv[1], sys.argv[2]
if mode == "snapshot":
    print("\n".join(sorted(sha(p) for p in files(root))))
elif mode == "plan":
    plan, wrong, seen = rows(root), 0, set()
    for r in plan:
        p = at(root, r["original_path"])
        seen.add(p)
        if not os.path.exists(p) or sha(p) != r["sha256"] or os.path.getsize(p) != int(r["bytes"]):
            wrong += 1
        if os.path.basename(r["proposed_path"]) != os.path.basename(r["original_path"]):
            wrong += 1
    disk = {os.path.normpath(p) for p in files(root)}
    print(f"rows {len(plan)}, wrong {wrong}, on disk and not planned {len(disk - seen)}, planned and not on disk {len(seen - disk)}")
    sys.exit(1 if wrong or disk != seen else 0)
elif mode == "after":
    with open(sys.argv[3]) as f:
        before = sorted(f.read().split())
    kinds = set(sys.argv[4:])
    now = sorted(sha(p) for p in files(root))
    moved = left = wrong = 0
    for r in rows(root):
        old, new = at(root, r["original_path"]), at(root, r["proposed_path"])
        if r["kind"] in kinds:
            moved += os.path.exists(new) and not os.path.exists(old)
            wrong += not (os.path.exists(new) and not os.path.exists(old))
        else:
            left += os.path.exists(old) and not os.path.exists(new)
            wrong += not (os.path.exists(old) and not os.path.exists(new))
    print(f"contents kept {now == before}, moved {moved}, left {left}, wrong {wrong}")
    sys.exit(0 if now == before and not wrong else 1)
