# Work out a refund path, and write only what is approved

`examples/travel-refund/claude-code.yaml` puts two agents on one folder of
booking records, both on the signed-in Claude Code program:

- `travel-research` reads the records and current official policy pages and
  explains what refund they support. It has no tool that writes.
- `travel-clerk` writes the one document a person approves, under `out/`.

Neither can run a command, send a message or submit a form. Asking the airline
is yours to do.

## Start the agents

Set both `claude-code.dir` fields to the folder, as an absolute path, sign in
to Claude Code on that host, and start the example:

```sh
humagen start -f examples/travel-refund/claude-code.yaml
```

Both profiles run the program in its restricted mode with an empty strict MCP
configuration: no shell, and file tools confined to the folder. The research
profile leaves the Write tool out. The clerk's has it, allowed under `out/`
only. The records are sent to the model provider as the program reads them.
The prompts tell both agents never to put a name, a booking reference or a
ticket number into a search or a URL; that is an instruction, not a control.

## Ask, then approve

Ask `travel-research`:

```text
Read the booking records in this folder. What happened to this trip, what does
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

Official pages do not always answer a program. In the checks here the transport
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

Give the checker the facts as the records write them:

```sh
python3 examples/travel-refund/check.py DIR 'EXA7Q2' 'EX 418' 'October 14, 2026' '$412.60'
```

It fails when a fact is not in a record or not in the request, or when
anything else was written under `out/`.

## What was checked, and what was not

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
- **Finding the itinerary in mail.** The records were files in a folder.
- **Sending the request, a form, a chargeback or a complaint.** No tool here
  does any of them.
