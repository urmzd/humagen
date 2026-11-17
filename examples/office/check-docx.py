"""Says exactly what differs between a document and its edited copy.

usage: uv run --with python-docx python check-docx.py ORIGINAL EDITED

Lists the package parts that differ, the text changed inside
word/document.xml, and every paragraph whose style, alignment or runs
(text, bold, italic, size, colour) differ. Anything not listed is the same.
"""
import difflib
import sys
import zipfile

from docx import Document

old, new = sys.argv[1], sys.argv[2]
a, b = zipfile.ZipFile(old), zipfile.ZipFile(new)
names = a.namelist()
print("same parts:", names == b.namelist())
print("parts that differ:", [n for n in names if n in b.namelist() and a.read(n) != b.read(n)])
xa, xb = a.read("word/document.xml").decode(), b.read("word/document.xml").decode()
for tag, i1, i2, j1, j2 in difflib.SequenceMatcher(None, xa, xb, autojunk=False).get_opcodes():
    if tag != "equal":
        print(" ", tag, repr(xa[i1:i2][:90]), "->", repr(xb[j1:j2][:260]))


def shape(path):
    out = []
    for p in Document(path).paragraphs:
        runs = []
        for r in p.runs:
            colour = str(r.font.color.rgb) if r.font.color and r.font.color.type else None
            runs.append((r.text, r.bold, r.italic, str(r.font.size), colour))
        out.append(repr((p.style.name, str(p.alignment), tuple(runs))))
    return out


sa, sb = shape(old), shape(new)
print("paragraphs:", len(sa), "->", len(sb))
for line in difflib.unified_diff(sa, sb, lineterm="", n=0):
    if line[0] in "+-" and not line.startswith(("+++", "---")):
        print(" ", line[:230])
