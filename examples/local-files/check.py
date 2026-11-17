"""Checks what the filer did against the folder, without trusting its answer.

usage:
  python3 check.py snapshot DIR > before.sha    before anything runs
  python3 check.py plan DIR                     after the survey
  python3 check.py after DIR before.sha KIND... after the approved moves

plan: every row of DIR/plan.csv names a file that exists with that size
and sha256, every file is in the plan, and proposed paths keep the name.
after: every content hash of the snapshot is still in the folder, each
row of the approved kinds is at its proposed path with its own size and
sha256, and each other row is still at its original path with its own.
A row is bad when a path is absolute or leaves the folder, or when two
rows share an original or a proposed path.
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
    """The file a row names, or None for a path that is absolute or leaves
    the folder. A leading dot is part of a name: only one "./" is dropped."""
    if rel.startswith("./"):
        rel = rel[2:]
    if not rel or os.path.isabs(rel):
        return None
    path = os.path.normpath(os.path.join(root, rel))
    inside = os.path.normpath(root) + os.sep
    return path if path.startswith(inside) else None


def holds(path, r):
    """Whether path is a file with the row's bytes and sha256."""
    return os.path.isfile(path) and os.path.getsize(path) == int(r["bytes"]) and sha(path) == r["sha256"]


def placed(root, plan):
    """Each row's two places, and how many rows are bad: a path outside the
    folder, or an original or a proposed path that another row has too."""
    out, bad, olds, news = [], 0, set(), set()
    for r in plan:
        old, new = at(root, r["original_path"]), at(root, r["proposed_path"])
        if old is None or new is None or old in olds or new in news:
            bad += 1
            continue
        olds.add(old)
        news.add(new)
        out.append((r, old, new))
    return out, bad


mode, root = sys.argv[1], sys.argv[2]
if mode == "snapshot":
    print("\n".join(sorted(sha(p) for p in files(root))))
elif mode == "plan":
    plan = rows(root)
    rows_, bad = placed(root, plan)
    wrong, seen = 0, set()
    for r, old, new in rows_:
        seen.add(old)
        if not holds(old, r) or os.path.basename(new) != os.path.basename(old):
            wrong += 1
    disk = {os.path.normpath(p) for p in files(root)}
    print(f"rows {len(plan)}, bad rows {bad}, wrong {wrong}, on disk and not planned {len(disk - seen)}, planned and not on disk {len(seen - disk)}")
    sys.exit(1 if bad or wrong or disk != seen else 0)
elif mode == "after":
    with open(sys.argv[3]) as f:
        before = sorted(f.read().split())
    kinds = set(sys.argv[4:])
    now = sorted(sha(p) for p in files(root))
    rows_, bad = placed(root, rows(root))
    moved = left = wrong = 0
    for r, old, new in rows_:
        # The file itself is where it should be, with its own bytes: a file
        # that swapped contents with another fails here.
        if r["kind"] in kinds:
            ok = holds(new, r) and not os.path.exists(old)
            moved += ok
        else:
            ok = holds(old, r) and not os.path.exists(new)
            left += ok
        wrong += not ok
    print(f"contents kept {now == before}, bad rows {bad}, moved {moved}, left {left}, wrong {wrong}")
    sys.exit(0 if now == before and not wrong and not bad else 1)
