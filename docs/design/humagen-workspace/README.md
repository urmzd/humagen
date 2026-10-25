# Humagen workspace design

This is the complete second-pass interaction concept, including the new
four-square mark, the project conversation shell, tool activity,
clarifications, approvals and native deliverables.

- [Interactive prototype](index.html)
- [Seven-page visual walkthrough](Humagen-interaction-system.pdf)
- [Vector mark](Humagen-mark.svg)
- [Editable sample files](exports/)
- [Application implementation and remaining system work](../../conversation-workspace.md)

Open `index.html` in a browser, or serve it locally from the repository root:

```sh
python3 -m http.server 4183 --bind 127.0.0.1 --directory docs/design/humagen-workspace
```

The prototype keeps its scripts, styles and sample downloads in one HTML
file. All agent execution, approvals, project moves and source states in
that concept are simulated and reset on reload. The PPTX, PDF, DOCX and CSV
downloads are real files. The production app uses the actual platform APIs;
its current behavior and the remaining backend work are described in the
implementation document linked above.

## Try the interaction loop

1. Expand the work summary and inspect a tool's result, input and output.
2. Open Needs you and answer Writer's audience question. Selecting an
   option does not submit it or approve any launch action.
3. Open the briefing next to the conversation, read a slide's notes, inspect
   its sources and enter presentation mode.
4. Request a change to one slide. The composer and posted message retain
   the artifact, version and slide reference.
5. Find the same result in Deliverables, alongside the document and table.

The JSON validation records describe 88 Chromium checks of this design
concept across 320, 390, 768 and 1440 pixel widths, plus 13 file checks. They
are retained as design review evidence, separate from the app's unit,
interaction, browser and platform tests.

## Regenerate the sample files and review

```sh
python3 -m venv /tmp/humagen-design-env
/tmp/humagen-design-env/bin/pip install -r docs/design/humagen-workspace/requirements.txt
/tmp/humagen-design-env/bin/python docs/design/humagen-workspace/generate-samples.py
/tmp/humagen-design-env/bin/python docs/design/humagen-workspace/generate-review.py
```

The scripts use the system DejaVu Sans fonts, available as
`fonts-dejavu-core` on Debian and Ubuntu. The review script reads the checked
screenshots and validation records and writes the PDF and an ignored ZIP.
The sample generator writes the editable decks and their PDFs, document,
CSV and structured sample data. To update the standalone prototype's
embedded downloads after regenerating them, run
`python docs/design/humagen-workspace/embed-samples.py`.

The original private source PDFs are not included. All checked-in outputs
use the sample launch scenario from the concept.

## Application review

These captures show the implemented console, separate from the concept:

- [Project conversation and participating agents](app-conversation.png)
- [Saved presentation inside the conversation](app-deliverable.png)
- [Source provenance on a phone](app-context-mobile.png)
- [Application validation and remaining limits](application-validation.md)

The conversation and source data come from an isolated offline server. The
Office preview cases supply sample artifact metadata and the real files in
`exports/` through explicit browser API fixtures; they exercise the app's
actual preview, version, presentation and revision interactions.
