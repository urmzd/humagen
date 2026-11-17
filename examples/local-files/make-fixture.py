"""Writes a generated folder to try the local files example on.

usage: python3 make-fixture.py DIR [BIG_MIB]

322 files in nested folders: invoices, photos, notes, reports, binaries
of no known kind, a name with spaces and accents, one exact duplicate
and two large files (30 MiB and BIG_MIB, 85 by default). The contents
are random and stand for nothing: this is not anyone's data.
"""
import os
import random
import sys

root = sys.argv[1]
big = int(sys.argv[2]) if len(sys.argv) > 2 else 85
random.seed(23)


def put(path, data):
    path = os.path.join(root, path)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as f:
        f.write(data)


for i in range(120):
    total = f"{random.randint(50, 900)}.{random.randint(0, 99):02d}"
    put(f"scan {i:03d} invoice ACME-{1000 + i}.txt", f"INVOICE ACME-{1000 + i}\nTotal: {total}\n".encode())
for i in range(90):
    put(f"IMG_{2000 + i}.jpg", b"\xff\xd8\xff\xe0" + random.randbytes(random.randint(2000, 9000)))
for i in range(60):
    put(f"notes/meeting-{i:02d}.md", f"# Meeting {i}\n- item {random.randint(1, 99)}\n".encode())
for i in range(40):
    rows = "\n".join(f"{random.randint(1, 9)},{random.randint(1, 9)}" for _ in range(50))
    put(f"old/2019/report_{i}.csv", ("a,b\n" + rows).encode())
for i in range(8):
    put(f"misc/blob{i}.bin", random.randbytes(4096))
put("Résumé final (2).txt", b"name with spaces and accents\n")
with open(os.path.join(root, "IMG_2001.jpg"), "rb") as f:
    put("dup/IMG_2001 copy.jpg", f.read())
put("big/archive-backup.tar", random.randbytes(1024 * 1024) * 30)
put("big/video-export.mov", random.randbytes(1024 * 1024) * big)
print("wrote 322 files under", root)
