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
- It reported that the transport department's refunds page refused it and that
  it read the refund rule as published in the Federal Register instead, in
  part. The refusal was the same when fetched separately, and the deadline it
  gave (7 business days for a credit card, 20 calendar days otherwise) is in
  that text.
- `travel-clerk`, with the approval, wrote `out/refund-request.md` and nothing
  else. Reference, flight, date, amount and ticket number matched the records,
  and what the records do not state was left as TO FILL. The records were
  unchanged.
- No address either agent opened held the passenger's name, the reference or
  the ticket number. The text of its searches was not inspected.

Not checked:

- **A real itinerary and a real airline.** The booking is invented, so no
  airline's own policy was read and nothing was asked of an airline.
- **The current codified rule.** The agent said it had not seen it. Treat the
  policy summary as a starting point, not as advice.
- **Finding the itinerary in mail.** The records were files in a folder.
- **Sending the request, a form, a chargeback or a complaint.** No tool here
  does any of them.
