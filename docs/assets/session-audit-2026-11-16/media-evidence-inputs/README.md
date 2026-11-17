# Generated media acceptance fixtures

These are made-up evidence, not a real incident or recording. The PDF is three image-only pages. The video is a schematic, and the illustration is AI generated. The M4A is macOS text-to-speech with silence between statements.

Keep these originals unchanged. Source hashes are in sources.json. The video captions name nominal intervals; actual decoded pixels change at source PTS4.2 and8.2 seconds. That discrepancy is intentional to retain the observed source faithfully. ASR outputs are uncertain and must not be treated as a literal or manually heard transcript.

To create the tested large-file variant without downloading 126MB:

```sh
ffmpeg -n -i scene.mp4 -c:v rawvideo -pix_fmt bgr24 large-scene.avi
```

The observed output is126,498,214bytes, rawvideo720x480,122frames at10fps,12.2seconds. Container metadata and hashes can vary across FFmpeg versions; inspect your actual output.
