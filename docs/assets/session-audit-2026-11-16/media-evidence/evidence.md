# Evidence report (GENERATED fixtures)

All inputs are **GENERATED fixtures**. Page/frame text says "GENERATED EVIDENCE FIXTURE" and "GENERATED SCHEMATIC VIDEO". The illustration carries no such text in its pixels; "generated" there is per the request only.
Originals unmodified. Derived files are in `ev_out/`. SHA-256 of originals:
- scanned-notes.pdf `5b124970181edc54c7bb86c35b948a7f8dcddb155fdff5b471110a3831ee4a8f`
- scene.mp4 `178d54e8ce6c9ffafe0bf57fe16e2ae4370d34d41425ccb12ab201fc5d5f06ea`
- illustration.png `1ff512e7d2b395b2aaec15caa1529b7cc7db46c603d99ce34b164dd05b5f0b55`
- recording.m4a `5e12cbb49b15a280ca4d02d5ab840bf882c8370c99127cc66e70ad45864766ab`

## Direct observations: PDF (3 pages, rendered at 80 dpi and viewed; no text layer, pdftotext returned nothing)
| PAGE | Visible content |
|---|---|
| 1 "Dispatch note" | Case GX-3F5AD3; Recorded date 17 Nov 2026; Receipt time 09:15; **Seal color: BLUE**; Receiver initials: JE; "Identity not verified." |
| 2 "Correction sheet" | Case GX-3F5AD3; Correction recorded 09:42; **Seal color: GREEN**; "Supersedes the first seal note."; "No payment proof attached." |
| 3 "Follow-up log" | Case GX-3F5AD3; Delivery planned Tuesday 09:30; Deposit requested 720 dollars; Payment status UNCONFIRMED; "Audio/video clocks not synchronized." |

## Direct observations: video (ffprobe: h264 720x480, 10 fps, 122 frames, duration 12.200 s, no audio stream)
Frames extracted at exact source times and viewed (contact sheets `ev_out/sheet.png`, `ev_out/b.png`). Center-pixel RGB scan of all 122 frames.
| Source time | Observed |
|---|---|
| 0.0 to 4.1 s (probed 1.0, 3.9, 4.0, 4.1) | Closed brown box, **blue** circle (RGB 22,92,203); caption "Source interval 00:00 to 00:04" |
| 4.2 s (frame 42) | First **green** frame (20,149,71); box lid open |
| 5.0, 7.9, 8.0, 8.1 s | Green circle, open lid; caption "00:04 to 00:08" |
| 8.2 s (frame 82) | First **red** frame (200,43,39); lid closed; caption "00:08 to 00:12" |
| 9.0 s, 12.1 s | Red circle |
Note: the color changes occur at 4.2 s and 8.2 s, not at 4.0/8.0 s. The captions at 4.0 to 4.1 s and 8.0 to 8.1 s say "00:00 to 00:04" / "00:04 to 00:08". It is a 0.2 s offset between caption and colour change. Not explained by the files.
No timestamp, clock, text of "09:xx", or document reference is visible in any inspected frame. I did not view every frame, only the probe frames above plus the center-pixel scan.

## Direct observations: illustration (1254x1254 PNG, viewed)
Watercolor-style painting of a red fox sitting in snow, head raised and turned right, wearing a knitted **blue** scarf with fringe. Pale blue sky with clouds, distant snowy hills, snow-covered bare shrubs, dry grass stalks. No text, seal, box, person, or document content visible.

## ASR (correlated ASR evidence, whisper-tiny.en, not proof of what was said)
Source identity: `recording.m4a` (AAC, mono, 16 kHz, 12.509 s). I re-ran ASR on the original (`ev_out/asr/recording.json`). Its text and word times match the pre-existing `transcript.json` (sha `90fab9e8...`), so that file is consistent with this source. I did not rely on the old clip WAVs: my re-extracted clips differ in bytes from them.
| SOURCE interval | ASR text (word timestamps in source time) |
|---|---|
| 2.46 to 7.00 s | "The revised delivery is Tuesday at $9.30." (full-file pass); "$9" 5.28 to 5.40 |
| 9.74 to 11.64 s | "The deposit is $720." |
| 11.64 to 12.32 s | "$20." repeated 5 times: likely ASR artifact over trailing audio, not meaningful |
Cross-check: isolated clips cut from the original (2.0 to 7.2 s and 9.5 to 11.9 s), re-transcribed, local times mapped back to source: "The revised delivery is Tuesday at 9.30." (words 2.00 to 5.90) and "The deposit is 720." (9.50 to 11.26). Same text; this is a second correlated ASR pass, not independent listening. "9.30" is a clock-like form but speech says "Tuesday at nine thirty" is only my reading of the ASR; the digits could be "9:30", or a price.
Unexamined: 0 to 2.46 s, 7.0 to 9.74 s of audio (ASR found no speech; not verified by listening or spectral check). The $20 tail after 11.64 s was not independently checked. No speaker identity examined.

## Answers
1. **Seal color superseding the first note: GREEN** (PAGE 2 states it supersedes the first seal note, which was BLUE on PAGE 1).
2. **Receiver identity: not established.** PAGE 1 gives initials "JE" and says "Identity not verified." Nothing in video, audio or image names or identifies a receiver.
3. **Payment: not proven.** PAGE 2 "No payment proof attached"; PAGE 3 "Payment status: UNCONFIRMED". The audio ASR mentions a deposit of $720 (9.74 to 11.64 s), and PAGE 3 says 720 dollars was *requested*. That matches a request, not a payment.
4. **Video-to-document clock sync: not established.** PAGE 3 states "Audio/video clocks not synchronized." Video has relative times only (0 to 12.2 s); document times (09:15, 09:42, 09:30) appear nowhere in the video.

## Contradictions and uncertainty
- Video shows blue then green then red. The PDF lists blue then green only. The red segment (8.2 s onward) has no document counterpart; I make no inference about it, and no link between video colors and the seal on the pages is established.
- Caption/colour-change offset of 0.2 s (see above).
- Audio is 12.509 s, video 12.200 s: different durations, consistent with the unsynchronized statement; alignment between them is unknown.
- Audio says "9.30"/Tuesday; PDF PAGE 3 says "Tuesday 09:30". Consistent in ASR text only, and ASR could be wrong. The audio is also said to be a "revised" delivery with no PDF page mentioning a revision of delivery.
- Illustration has no demonstrated relation to the other files (its blue scarf is only a visual coincidence with "BLUE").
- No missing events inferred.
