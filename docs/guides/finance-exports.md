# Reconcile account exports and draft a sourced plan

`examples/finance-exports/claude-code.yaml` runs an agent on the signed-in
Claude Code program in a folder of account exports on this host. A script does
the arithmetic: it removes duplicate rows, reconciles each export's balances
and totals the months and the debts. The agent runs it, reports what does not
reconcile, and writes a plan in which every amount is copied from the script's
output with its source. It orders what the numbers show. It gives no financial
advice, and nothing here reaches a bank or moves money.

## Prepare the folder

Put in one folder the exports you authorize, a file `authorized.txt` naming
them one a line, and a copy of `examples/finance-exports/reconcile.py`. An
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

## Start the agent

Set `claude-code.dir` to the folder, as an absolute path, sign in to Claude
Code on that host, and start the example:

```sh
humagen start -f examples/finance-exports/claude-code.yaml
```

The profile runs the program in its restricted mode with an empty strict MCP
configuration. File tools are confined to the folder and writing is allowed
under `out/` only. Of shell commands, `python3 reconcile.py` is allowed. A
command that changes something else is refused. The program still runs commands
it treats as read-only, such as `ls`. So `authorized.txt` is an instruction to
the agent, not a lock: a file it must not see should not be in the folder. The
exports are sent to the model provider as the program reads them.

## Ask for the reconciliation and the plan

```text
Reconcile the authorized exports in this folder. Tell me how many transactions
there are once duplicates are removed and where the duplicates came from,
which exports do not reconcile and by how much, and what each debt is, with
its rate and minimum. Then write a plan to out/plan.csv: the monthly figures
and debts copied from the summary with their source, and your suggestions for
which debt to pay down first and why as proposals. Say what the exports do not
show.
```

A plan row that carries an amount names its source as `out/summary.csv:N`, the
line of the summary it copies, the header being line 1.

## Check it yourself

```sh
python3 examples/finance-exports/check.py DIR
```

The checker shares no code with the script: its rules are written out a second
time, including the words that give a category. From the exports it rebuilds
every transaction with its date, description, amount, category, the file and
line it is first on and the lines of its exact copies, and the balance
differences and the monthly and debt figures. It fails when anything under
`out/` differs field for field, when a row comes from a file that is not
authorized, when copies of a transaction disagree, when an amount is not whole
cents, when a plan amount is not the summary line its source names, or when a
proposal carries an amount.

```sh
python3 examples/finance-exports/regress.py
```

runs both against twenty cases that must pass or fail: the generated exports
untouched, fourteen changes to what was written, and five changes to the
exports themselves.

## What was checked, and what was not

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
- With the profile's flags, `python3 reconcile.py` ran, `python3 -c
  "print(1)"` was refused, and `ls -la` ran.

Not checked:

- **Real exports.** Every name and number is invented. A bank's own export
  format has to be put into the format above first, and no bank's format was
  tried.
- **Whether the plan is good advice.** Nobody qualified reviewed it.
- **Categories.** The script assigns them from words in the description. A
  description it does not know is `other`.
- **Interest going forward, currencies, joint or business accounts, taxes.**
- **Anything in a spreadsheet or a cloud folder.** The exports were files.
