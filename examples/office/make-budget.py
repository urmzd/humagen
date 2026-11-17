"""Writes three saved revisions of a generated budget workbook.

usage: uv run --with openpyxl python make-budget.py DIR

budget-v1.xlsx is right. Revision 2 adds an expense row its total does not
cover. Revision 3 replaces the net formula with a typed number. The figures
are made up, and the files hold formulas with no cached values.
"""
import os
import sys

import openpyxl
def book(v):
    wb=openpyxl.Workbook(); inc=wb.active; inc.title='Income'
    inc.append(['Source','Amount']); rows=[('Salary',5200),('Freelance',900 if v<3 else 1100),('Interest',40)]
    for r in rows: inc.append(r)
    inc.append(['Total','=SUM(B2:B4)'])
    ex=wb.create_sheet('Expenses'); ex.append(['Item','Amount'])
    items=[('Rent',1800),('Food',650),('Transport',210),('Utilities',180),('Insurance',260)]
    if v>=2: items.append(('Software',240))
    for r in items: ex.append(r)
    ex.append(['Total','=SUM(B2:B6)'])   # v2 and v3: range not extended to the new row
    s=wb.create_sheet('Summary'); s.append(['Income','=Income!B5'])
    s.append(['Expenses',f'=Expenses!B{len(items)+2}'])
    s.append(['Net','=B1-B2' if v<3 else 3100])   # v3: a typed number replaces the formula
    s.append(['Savings rate','=B3/B1'])
    wb.save(os.path.join(sys.argv[1], f'budget-v{v}.xlsx'))
for v in (1,2,3): book(v)
