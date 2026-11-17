# Edit a document and audit a workbook

`examples/office/claude-code.yaml` runs an agent on the signed-in Claude Code
program in a folder of Office files on this host. It edits a `.docx` in place,
run by run, so the parts it was not asked to change stay as they were, and it
audits `.xlsx` workbooks by reading their formulas and recomputing the totals.
Humagen's own documents and spreadsheets are not involved: the files stay
files.

## Start the agent

Install [uv](https://docs.astral.sh/uv/) on the host. The agent runs
`python-docx` and `openpyxl` through it, and uv fetches each the first time.
Set `claude-code.dir` to the folder, as an absolute path, sign in to Claude
Code on that host, and start the example:

```sh
humagen start -f examples/office/claude-code.yaml
```

The profile limits the program's built-in tools to Bash, Read, Write and Glob,
preauthorizes them, and sets an empty strict MCP configuration. The program
runs as you. Humagen does not confine it to `dir`, and the native sandbox and
tool grants do not apply. The program's own user settings, hooks, plugins and
skills still load.

## Edit a document

Name the file, each change, and what must not change. Ask for a new file:

```text
Edit resume.docx and save the result as resume-updated.docx; leave resume.docx
as it is. Three changes only: the phone number becomes 555-0199; the current
job title "Senior Engineer" becomes "Staff Engineer"; add a third bullet under
that job, in the same bullet style. Everything else must stay exactly as it
is. Tell me what you changed and what you could not verify.
```

Then check the result yourself. `examples/office/check-docx.py` lists every
difference between the two files, down to the run:

```sh
uv run --with python-docx python examples/office/check-docx.py resume.docx resume-updated.docx
```

A structural comparison is not a look at the page. Open both files, or render
them. On a Mac, Quick Look renders a page without opening an application:

```sh
qlmanage -t -s 1600 -o out resume.docx resume-updated.docx
```

## Audit a workbook

Give the revisions as files, and ask for cells:

```text
budget-v1.xlsx, budget-v2.xlsx and budget-v3.xlsx are three saved revisions of
one budget. Read every sheet and every formula, recompute the totals yourself,
and reconcile Income, Expenses and Net in each revision. For each problem give
the file, sheet, cell, what is in the cell, what it should be, the correct
number, and the revision that introduced it. List every cell that changed
between consecutive revisions. Write the findings to audit.md.
```

Check that the workbooks were not touched (`shasum` before and after), and
check the numbers: the audit names the cells, so each one can be opened.

## What was checked, and what was not

Checked on generated files from `examples/office/make-resume.py` and
`examples/office/make-budget.py`:

- **Document.** Three edits to a styled resume. Only `word/document.xml`
  differed, by those three edits. Every other part was byte-identical, every
  other paragraph kept its style, alignment and runs, and the original file
  was untouched. Quick Look renders of both pages were read: the three changes
  and nothing else.
- **Workbook.** Both seeded defects found with their cells and revisions: an
  expense total whose range leaves out a row added in revision 2, and a net
  figure typed over its formula in revision 3. The cells changed between
  revisions were listed, and the workbooks were byte-identical afterwards.

Not checked:

- **Real templates and budgets.** The files are generated and their contents
  are made up.
- **Word and Excel.** Neither was opened. Quick Look is not Word: it did not
  draw the table's grid style, and it drew the footer under the text. Both
  renders had the same limits.
- **Tracked changes, comments, images, text boxes, slides.** Not exercised.
- **Revision history in Drive or OneDrive.** The revisions were three files.
- **Calculation.** The agent recomputed sums itself. Macros, pivot tables and
  functions beyond the fixture's were not exercised, and cached values from
  Excel were not used.
