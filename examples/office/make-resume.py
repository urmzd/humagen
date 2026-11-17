"""Writes resume.docx, a generated styled document to try the office example on.

usage: uv run --with python-docx python make-resume.py DIR

A centred title, a coloured italic line, bold runs, bullet lists, a styled
table and a footer. The person and the employers are made up.
"""
import os
import sys

from docx import Document
from docx.shared import Pt, RGBColor
from docx.enum.text import WD_ALIGN_PARAGRAPH
d=Document()
h=d.add_heading('Jordan Example',0); h.alignment=WD_ALIGN_PARAGRAPH.CENTER
p=d.add_paragraph(); p.alignment=WD_ALIGN_PARAGRAPH.CENTER
r=p.add_run('Product Engineer'); r.italic=True; r.font.size=Pt(13); r.font.color.rgb=RGBColor(0x33,0x55,0x99)
p=d.add_paragraph(); p.alignment=WD_ALIGN_PARAGRAPH.CENTER
p.add_run('Phone: ').bold=True; p.add_run('555-0100'); p.add_run('   Email: ').bold=True; p.add_run('jordan@example.com')
d.add_heading('Experience',1)
p=d.add_paragraph(); r=p.add_run('Senior Engineer'); r.bold=True; p.add_run(', Northwind Labs'); r=p.add_run('   2021 to present'); r.italic=True
for t in ('Led the checkout rewrite, cutting failures by 40 percent.','Mentored four engineers.'): d.add_paragraph(t,style='List Bullet')
p=d.add_paragraph(); r=p.add_run('Engineer'); r.bold=True; p.add_run(', Contoso'); r=p.add_run('   2017 to 2021'); r.italic=True
d.add_paragraph('Built the billing export pipeline.',style='List Bullet')
d.add_heading('Skills',1)
t=d.add_table(rows=3,cols=2); t.style='Light Grid Accent 1'
for i,(a,b) in enumerate([('Area','Detail'),('Languages','Go, TypeScript, Python'),('Systems','Postgres, Kubernetes')]):
    t.cell(i,0).text=a; t.cell(i,1).text=b
t.cell(0,0).paragraphs[0].runs[0].bold=True; t.cell(0,1).paragraphs[0].runs[0].bold=True
s=d.sections[0]; s.footer.paragraphs[0].text='References on request'
d.save(os.path.join(sys.argv[1], 'resume.docx'))
