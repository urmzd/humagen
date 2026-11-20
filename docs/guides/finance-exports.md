# Reconcile account exports and draft a sourced plan

`examples/finance-exports/humagen.yaml` runs a native agent, `ledger`, on the
account exports in one conversation. A script does the arithmetic: it removes
duplicate rows, reconciles each export's balances and totals the months and
the debts. The agent runs it with the shell tool in a sandbox, reports what
does not reconcile, and writes a plan in which every amount is copied from
the script's output with its source. It orders what the numbers show. It
gives no financial advice, and nothing here reaches a bank or moves money.

Before #383 this example ran on the Claude Code program in a folder on the
host, whose allow rules refused every command but the script and every write
outside `out/`. Those rules are gone with it; see below for what holds now.

## Prepare the folder

Put in one folder the exports you authorize, a file `authorized.txt` naming
them one a line, and a copy of `examples/finance-exports/reconcile.py`. These
go into the conversation, so leave out any export the agent must not read. An
export is a CSV that starts with `# key: value` lines (`account`, `kind` as
`bank`, `card` or `loan`, `period_start`, `period_end`, `opening_balance`,
`closing_balance`, and for a debt `apr_percent` and `minimum_payment`),
followed by the header `id,date,description,amount`. Money in is positive. Two
rows are one transaction when they have the same account and id, and nothing
else is merged, so two purchases of the same amount on one day both stay. An
amount is digits with at most two decimals. The script stops, and writes no
figures, on an amount written any other way and on two rows with one account
and id that disagree on date, description or amount: it lists those in
`out/conflicts.csv` and leaves the choice to you.

## Start the agent and add the files

The profile runs each run's tools in a Docker instance of
`python:3.13-alpine`, so the host needs a running Docker daemon and that
image (`docker pull python:3.13-alpine`). Set a provider key in the shell
(`ANTHROPIC_API_KEY`, `OPENAI_API_KEY` or `GOOGLE_API_KEY`; the agent tries
them in that order) and start the example:

```sh
humagen start -f examples/finance-exports/humagen.yaml
```

In another terminal, put the folder's files into a new conversation with the
files API (`examples/conversation-files.sh` starts a conversation no agent
answers yet and saves each file under its own name):

```sh
python3 examples/finance-exports/make-fixture.py exports   # or your own folder
thread=$(sh examples/conversation-files.sh new "Exports")
sh examples/conversation-files.sh put "$thread" exports/authorized.txt exports/reconcile.py \
  $(grep -v -e '^#' -e '^$' exports/authorized.txt | sed 's|^|exports/|')
```

That puts only the exports `authorized.txt` names. The generated folder also
holds `other-person-savings.csv`, an export that is not authorized, and it
stays out of the conversation. The files API takes UTF-8 text only, at most
2 MiB a file. Open the conversation in the
console, or post to it with `POST /api/threads/{id}/messages`, and tag
`@ledger`.

What holds now:

- The agent has `file_list`, `file_read`, `file_write`, `shell` and
  `publish_artifact`. The shell runs in the run's instance, which has no
  network and is destroyed when the run ends; what is written under `out/` is
  kept with the conversation. The instance runs with `runc`, which shares the
  host's kernel: use it for trusted local trials only.
- Inside the instance the shell can run any command and write anywhere in the
  working directory. "Run only `python3 reconcile.py`", `authorized.txt` and
  "only under `out/`" are instructions in the prompt, not controls. A file the
  agent must not see should not be in the conversation.
- The `local` and `unisolated` sandbox kinds do not run this: the local kind's
  shell starts with an empty environment and no `PATH`, and unisolated refuses
  the shell.
- A native turn makes at most 10 rounds of tool calls.
- The exports are sent to the model provider as the agent reads them.

## Ask for the reconciliation and the plan

```text
Reconcile the authorized exports in this conversation. Tell me how many
transactions there are once duplicates are removed and where the duplicates
came from, which exports do not reconcile and by how much, and what each debt
is, with its rate and minimum. Then write a plan to out/plan.csv: the monthly figures
and debts copied from the summary with their source, and your suggestions for
which debt to pay down first and why as proposals. Say what the exports do not
show.
```

A plan row that carries an amount names its source as `out/summary.csv:N`, the
line of the summary it copies, the header being line 1.

## Check it yourself

Fetch what was written into the folder that holds the exports, then check it
there:

```sh
sh examples/conversation-files.sh get "$thread" exports out/transactions.csv out/reconciliation.csv out/summary.csv out/plan.csv
python3 examples/finance-exports/check.py exports
```

The checker shares no code with the script: its rules are written out a second
time, including the words that give a category. From the exports it rebuilds
every transaction with its date, description, amount, category, the file and
line it is first on and the lines of its exact copies, and the balance
differences and the monthly and debt figures. It fails when anything under
`out/` differs field for field, when a row comes from a file that is not
authorized, when copies of a transaction disagree, when an amount is not whole
cents, when the summary is not exactly the rows the exports give (each month
and category, and for each debt its balance, rate, date and file from the
statement with the latest period end, and its minimum payment), when a plan
amount is not the summary line its source names, or when a proposal carries an
amount.

```sh
python3 examples/finance-exports/regress.py
```

runs both against twenty-six cases that must pass or fail: the generated
exports untouched, nineteen changes to what was written, and six changes to
the exports themselves.

## What was checked, and what was not

The checks below ran before #383, on the Claude Code path this profile
replaces. On the native profile: it starts and passes `humagen config check`;
`reconcile.py` ran unchanged in `python:3.13-alpine` with no network (33
transactions, 4 duplicate rows removed, the card export reported as not
reconciling); and `regress.py` passes its 26 cases. It has not been run with a
live model, so the agent running the script through the shell tool is not yet
shown: rerun the prompt and the checker before relying on it.

Checked on five invented exports from `examples/finance-exports/make-fixture.py`:
two checking exports that overlap by two weeks, a card statement whose closing
balance is 25.00 away from its rows, a loan, and a fifth export that is in the
folder and not authorized.

- 33 transactions after 4 duplicate rows were removed, all from the overlap.
  The two coffees of one day stayed two.
- The card statement was reported as not reconciling, by 2,500 cents, and not
  explained away. The others reconciled.
- The plan copied 26 amounts from the summary, each naming its line, and made
  proposals with no amount, among them the debt with the higher rate first and
  asking the card's issuer about the difference.
- The export that is not authorized was not opened, and the exports were
  unchanged.
- On a copy in which one transaction's second copy has another amount, with
  the balance adjusted so that the export still reconciles, the script stopped,
  and the agent reported the two lines, chose neither and wrote no plan.
- With the removed profile's allow rules, `python3 -c "print(1)"` and writes
  outside `out/` were refused. The native profile refuses neither.

Not checked:

- **Real exports.** Every name and number is invented. A bank's own export
  format has to be put into the format above first, and no bank's format was
  tried.
- **Whether the plan is good advice.** Nobody qualified reviewed it.
- **Categories.** The script assigns them from words in the description. A
  description it does not know is `other`.
- **Interest going forward, currencies, joint or business accounts, taxes.**
- **Anything in a spreadsheet or a cloud folder.** The exports were files.
