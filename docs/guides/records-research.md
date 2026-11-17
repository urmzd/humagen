# Build a sourced chronology from records

`examples/records-research/claude-code.yaml` runs an agent on the signed-in
Claude Code program in a folder of records on this host. It reads every record,
writes a chronology in which each dated event names its record and quotes the
words that carry the date, lists what the records mention and do not prove, and
opens current official pages for a rule or a fee. It reports what the records
and the pages say. It gives no legal advice.

## Start the agent

Set `claude-code.dir` to the folder, as an absolute path, sign in to Claude
Code on that host, and start the example:

```sh
humagen start -f examples/records-research/claude-code.yaml
```

The profile runs the program in its restricted mode with Read, Glob, Grep,
Write, WebSearch and WebFetch and an empty strict MCP configuration: no shell,
and file tools confined to the folder. It may write only under `out/`: a write
anywhere else in the folder is refused. The records are sent to the model
provider as the program reads them. The prompt tells the agent never to put a
name or a case number into a search or a URL; that is an instruction, not a
control, so read the run's tool events if it matters.

## Ask for records and quotes

```text
Build a chronology of this application from the records in this folder. Write
out/chronology.csv with the columns date, event, source_file, quote,
certainty: quote copied exactly from the record; date only what that quote
states, as YYYY-MM-DD, or ????-MM-DD when it gives a day and month and no
year, or the word unknown; certainty one of stated, conflicting, unknown. Write
out/unknowns.md listing what the records mention and do not prove, and where
they disagree. Then open the official page for the current filing fee for
this form and tell me whether the amount paid matches the fee for filing
online today, with the exact URL you opened.
```

## Check it yourself

```sh
python3 examples/records-research/check.py DIR
```

It fails when a row names a file that is not in the folder, quotes words that
are not in that file, or carries a date its own quote does not state: a full
date needs the day, month and year in the quote, `????-MM-DD` needs a day and
month there and no year, and anything else must be `unknown`. So a likely date
borrowed from another record fails, and so does a certainty that is not exactly
one of the three words. `python3 examples/records-research/check.py
--self-test` runs its own cases. It reads dates written as `March 4, 2025`,
`3/4/2025` (month first) and `2025-03-04`; a date written another way has to be
`unknown`, and it does not judge the event text. For a page, open the URL the
answer gives and find the sentence.

## What was checked, and what was not

Checked on six invented records from `examples/records-research/make-fixture.py`,
an application with three traps: a request for evidence that an email mentions
and the folder does not hold, a response whose mailing nothing proves, and a
note that disagrees with a notice about a day.

- 13 rows, each quote found word for word in its record, every record cited,
  and every date stated by its own quote: eight full dates, three with a day
  and month and no year, and two unknown.
- The disagreement was reported with both records. The missing request, the
  unproven mailing, the undated interview and the missing decision were listed
  as unknown.
- The fee sentence it quoted from the official page was on that page when
  fetched separately, at the URL it gave.
- A write outside `out/` was refused, and the records were unchanged.

An earlier run, before the checker read dates, gave a note that says "4/3" the
date of the appointment the day before, and gave "June 6" the year of the
records around it. Both now fail the checker, and the prompt now asks for
`????-MM-DD`.

Not checked:

- **Real records.** The set is invented. Nothing here covers scans,
  photographs, PDFs, handwriting or a large case file.
- **Legal correctness.** Nobody qualified reviewed the output.
- **Pages behind a sign-in**, and any official page other than the one opened.
- **Sending or filing anything.** The profile has no tool for it.
