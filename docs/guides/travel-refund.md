# Work out a refund path, and write only what is approved

`examples/travel-refund/humagen.yaml` puts two native agents on one
conversation's booking records:

- `travel-research` reads the records and current official policy pages and
  explains what refund they support. It has `file_list`, `file_read`,
  `web_search` and `web_fetch`, and no tool that writes.
- `travel-clerk` writes the one document a person approves. It also has
  `file_write` and `publish_artifact`.

Neither has a shell, and neither can send a message or submit a form. Asking
the airline is yours to do. Before #383 both ran on the Claude Code program in
a folder on the host.

## Start the agents and add the records

Set a provider key in the shell (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY` or
`GOOGLE_API_KEY`; each agent tries them in that order) and start the example:

```sh
humagen start -f examples/travel-refund/humagen.yaml
```

In another terminal, put the records into a new conversation with the files
API (`examples/conversation-files.sh` starts a conversation no agent answers
yet and saves each file under its own name):

```sh
python3 examples/travel-refund/make-fixture.py booking   # or your own folder
thread=$(sh examples/conversation-files.sh new "Refund")
sh examples/conversation-files.sh put "$thread" booking/*
```

The files API takes UTF-8 text only, at most 2 MiB a file. Open the
conversation in the console, or post to it with
`POST /api/threads/{id}/messages`, and tag the agent you mean.

The clerk's `file_write` can write anywhere in the conversation's working
directory, so "only under `out/`" is an instruction in its prompt, not a
control; the conversation holds copies, and your folder is not touched. The
records are sent to the model provider as the agents read them. The prompts
tell both agents never to put a name, a booking reference or a ticket number
into a search or a URL; that is an instruction, not a control. A native turn
makes at most 10 rounds of tool calls.

## Ask, then approve

Ask `travel-research`:

```text
Read the booking records in this conversation. What happened to this trip, what does
the current official policy say about a refund in this situation, and what
refund path do the records and the policy support? Quote the itinerary facts
with their record, cite the exact official URL you opened, and say what the
records do not show.
```

Read the answer and open the page it cites. If you decide to ask for the
refund, approve the one document, to `travel-clerk`:

```text
I approve one action: write the refund request for this cancelled booking to
out/refund-request.md, addressed to the airline, asking for the full amount
paid back to the original payment method. Use only facts from the records. Do
not send it.
```

## When the official page refuses

Official pages do not always answer an automated reader. In the checks here the transport
department's refunds page answered 403 and the electronic regulations site sent
an unblock page. The research agent is told what to read instead: the
regulation in the annual Code of Federal Regulations edition on govinfo.gov,
which does answer. For refunds of air fares in the United States that is 14 CFR
Part 260:

```text
https://www.govinfo.gov/content/pkg/CFR-2026-title14-vol4/xml/CFR-2026-title14-vol4-part260.xml
```

An annual edition is the text as of its revision date, January 1 of its year
for this title. A change made after that date is not in it. The agent is told
to name the edition and that date, and to say that no policy was verified when
no official text opens. A search result or a news page is not a source.

## Check it yourself

Fetch the request into the folder that holds the records, then give the
checker the facts as the records write them:

```sh
sh examples/conversation-files.sh get "$thread" booking out/refund-request.md
python3 examples/travel-refund/check.py booking 'EXA7Q2' 'EX 418' 'October 14, 2026' '$412.60'
```

Check the conversation's file list as well (`GET /api/threads/{id}/files`):
the checker sees only what you fetched.

It fails when a fact is not in a record or not in the request, or when
anything else was written under `out/`.

## What was checked, and what was not

The checks below ran before #383, on the Claude Code path this profile
replaces. The native profile starts and passes `humagen config check`; it has
not been run with a live model. `web_fetch` returns at most 40,000 characters
of a page, so a long regulation comes back cut: rerun the prompts and the
checker before relying on it.

Checked on three invented records from `examples/travel-refund/make-fixture.py`:
a booking, the airline's cancellation notice and the passenger's notes.

- `travel-research` quoted the itinerary from its records and named what they
  do not show (no proof the rebooking was declined, no refund request, whether
  the rule applies to this airline). Asked to save a request as well, it could
  not, and said so.
- In a first run it reported that the transport department's refunds page
  refused it, read the rule's Federal Register publication in part, and said it
  had not seen the codified text. The refusal was the same when fetched
  separately.
- With the fallback above, it opened only the 2026 annual edition of 14 CFR
  Part 260 on govinfo.gov, said its dates read 2026-01-01 and that its source
  note runs to an amendment of August 12, 2024, and quoted the sections on a
  cancelled flight, a full and prompt refund, 7 business days for a credit card
  and the original form of payment. Each quotation is in that text when fetched
  separately. It ran no search and opened no other site.
- An earlier answer led with a full refund as if it were settled, although the
  records do not show that the rule covers this airline or who sold the
  ticket, and it said that filing a request would settle the deadline's start,
  which no passage it had read says: the rule's deadline refers to a section
  that makes a refund due on events such as declining a rebooking. The prompt
  now requires conditions first, a quoted passage for the period and for what
  starts it, and a refund due on an event kept apart from a request filed.
  The answer after that change named three conditions before any entitlement,
  quoted the period and the section it refers to, and said it could not quote
  what starts the clock. Its reader returns summaries with short quotations,
  so long passages are not quoted in full. This is a limit of the output, not
  a finding about the rule: read the cited text yourself.
- `travel-clerk`, with the approval, wrote `out/refund-request.md` and nothing
  else. Reference, flight, date, amount and ticket number matched the records,
  and what the records do not state was left as TO FILL. The records were
  unchanged.
- No address either agent opened held the passenger's name, the reference or
  the ticket number. The text of the first run's two searches was not
  inspected.

Not checked:

- **A real itinerary and a real airline.** The booking is invented, so no
  airline's own policy was read and nothing was asked of an airline.
- **Whether the summary is legally right.** Nobody qualified reviewed it.
- **Changes after the edition's date,** and whether the airline in a real
  booking is one the rule covers. Treat the summary as a starting point, not
  as advice.
- **Finding the itinerary in mail.** The records were files. The
  [mail profile](mail-calendar.md) reads mail through a person's own
  connection, not exercised with this one.
- **Sending the request, a form, a chargeback or a complaint.** No tool here
  does any of them.
