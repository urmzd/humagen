# Read and organize a local folder

`examples/local-files/claude-code.yaml` runs an agent on the signed-in Claude
Code program with its work directory set to a folder on this host. The agent
surveys every file where it is, with its original name, writes a plan, and
changes nothing until a person approves it. Files are read in place: they are
not uploaded, so the size limits of scratch files and attachments do not apply.

## Start the filer

Set `claude-code.dir` to the folder, as an absolute path, and sign in to Claude
Code on that host. Then start the example:

```sh
humagen start -f examples/local-files/claude-code.yaml
```

The profile limits the program's built-in tools to Bash, Read, Write, Glob and
Grep, preauthorizes them, and sets an empty strict MCP configuration. The
program runs as you. Humagen does not confine it to `dir`: the shell can reach
whatever your account can, and the native sandbox and tool grants do not apply.
In the check below it wrote its helper script beside the folder, not in it.
The program's own user settings, hooks, plugins and skills still load. Use it
on a host and a folder you trust it with.

## Survey, then approve

Ask for the survey first, and say that nothing may change:

```text
Survey every file under this folder, including subfolders and the large ones.
Change nothing yet. Write plan.csv with one row per file: original_path,
bytes, sha256, kind, proposed_path, reason. Then tell me the file count, the
count per kind, the duplicates, and everything you did not read or could not
classify.
```

Read `plan.csv`. Then approve all of it or a part, in the same conversation:

```text
Approved in part. Apply only the invoice and photo rows. Do not touch the
rest. Write changes.log with one line per move and the sha256 before and
after. Report what you moved, what you left and why, and any move that failed.
```

The plan and the log stay in the folder as the record of what was proposed and
what was done. The answer lists what was not read: ask for that every time.

## Check it yourself

`examples/local-files/check.py` checks the folder without trusting the answer:

```sh
python3 examples/local-files/check.py snapshot DIR > before.sha
python3 examples/local-files/check.py plan DIR
python3 examples/local-files/check.py after DIR before.sha invoice photo
```

`plan` fails when a row's size or hash is not the file's, or a file is missing
from the plan. `after` fails when a content hash of the snapshot is gone, an
approved row is not at its proposed path with its own size and hash, or another
row is not still at its original path with its own. Both fail on a row whose
path is absolute or leaves the folder, and on two rows that share a path. A
name that starts with a dot is kept as it is.

## What was checked, and what was not

Checked on a generated folder from `examples/local-files/make-fixture.py`: 322
files, 117 MiB, nested folders, names with spaces and accents, one exact
duplicate, a 30 MiB and an 85 MiB file. The survey changed nothing and its 322
rows matched the folder. After a partial approval, 211 files had moved, 111 had
not, no other file had moved, and every content hash was still present.

Not checked:

- **Real folders.** The fixture's contents are random. Nothing here says how
  the agent classifies real documents.
- **Cloud folders.** A Drive or OneDrive folder is reached through a source
  connector, not this profile.
- **Another host.** The folder must be on the host that runs the program.
- **The console.** The plan is a file in the folder. No page shows it.
- **Deletion and renaming.** The example's prompt forbids both.
