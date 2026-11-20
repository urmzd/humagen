# Build a sourced chronology from records

`examples/records-research/humagen.yaml` runs a native agent, `records`, on
the records in one conversation. It reads every record, writes a chronology in
which each dated event names its record and quotes the words that carry the
date, lists what the records mention and do not prove, and opens current
official pages for a rule or a fee with `web_search` and `web_fetch`. It
reports what the records and the pages say. It gives no legal advice.

Before #383 this example ran on the Claude Code program in a folder of records
on the host. No confined way reads a host folder in place now: only the
`local` sandbox kind's shell could, unconfined and with the server's
privileges, which is why the local-files example was removed rather than
ported. So the records go into the conversation.

## Start the agent and add the records

Set a provider key in the shell (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY` or
`GOOGLE_API_KEY`; the agent tries them in that order), then start the example:

```sh
humagen start -f examples/records-research/humagen.yaml
```

In another terminal, put the records into a new conversation's working
directory with the files API. `examples/conversation-files.sh` starts a conversation no agent answers
yet and saves each file under its own name:

```sh
python3 examples/records-research/make-fixture.py records   # or your own folder
thread=$(sh examples/conversation-files.sh new "Records")
sh examples/conversation-files.sh put "$thread" records/*
```

The files API takes UTF-8 text only, at most 2 MiB a file, and a conversation
holds at most 200 files and 20 MiB. A scan, photo or PDF record cannot go in
this way: attach it to a message instead, for a model that reads it
([attachments](attachments.md)). Open the conversation in the console, or post
to it with `POST /api/threads/{id}/messages`, and tag `@records`.

The agent is granted `file_list`, `file_read`, `file_write`,
`publish_artifact`, `web_search` and `web_fetch`, and nothing else: no shell
and no way to send anything. `file_write` can write anywhere in the
conversation's working directory, so "only under `out/`" is an instruction in
its prompt, not a control; the conversation holds copies, and your folder is
not touched. The records are sent to the model provider as the agent reads
them. The prompt tells the agent never to put a name or a case number into a
search or a URL; that is an instruction too, so read the run's tool events if
it matters. A native turn makes at most 10 rounds of tool calls: with many
records, ask for the chronology in parts.

## Ask for records and quotes

```text
Build a chronology of this application from the records in this
conversation. Write
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

Fetch what the agent wrote into the folder that holds the records, then check
it there:

```sh
sh examples/conversation-files.sh get "$thread" records out/chronology.csv out/unknowns.md
python3 examples/records-research/check.py records
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

The checks below ran before #383, on the Claude Code path this profile
replaces. The native profile starts and passes `humagen config check`, and
`conversation-files.sh` was checked against a running server (a conversation
started with no run, six records put, two fetched back byte for byte). It has
not been run with a live model: rerun the prompt above and the checker before
relying on it.

Before #383, the removed Claude Code profile was checked on six invented
records from `examples/records-research/make-fixture.py`, an application with
three traps: a request for evidence that an email mentions and the folder does
not hold, a response whose mailing nothing proves, and a note that disagrees
with a notice about a day.

- (Before #383, Claude Code profile) 13 rows, each quote found word for word
  in its record, every record cited, and every date stated by its own quote:
  eight full dates, three with a day and month and no year, and two unknown.
- (Before #383, Claude Code profile) The disagreement was reported with both
  records. The missing request, the unproven mailing, the undated interview
  and the missing decision were listed as unknown.
- (Before #383, Claude Code profile) The fee sentence it quoted from the
  official page was on that page when fetched separately, at the URL it gave.
- (Before #383, Claude Code profile) A write outside `out/` was refused, and
  the records were unchanged. The native profile has no such refusal: see
  above.

An earlier run on that profile, before the checker read dates, gave a note
that says "4/3" the date of the appointment the day before, and gave "June 6"
the year of the records around it. Both now fail the checker, and the prompt
now asks for `????-MM-DD`.

Not checked:

- **Real records.** The set is invented. Nothing here covers scans,
  photographs, PDFs, handwriting or a large case file.
- **Legal correctness.** Nobody qualified reviewed the output.
- **Pages behind a sign-in**, and any official page other than the one opened.
- **Sending or filing anything.** The profile has no tool for it.
