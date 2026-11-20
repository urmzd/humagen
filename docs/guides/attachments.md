# Attach files to a conversation

Use Attach in the composer's plus menu to select images, PDFs, Office files,
audio or video. A message accepts five files with at most 25 MiB of decoded
bytes in total. The text-file picker retains its 200 KiB cap. Files remain with
the draft after a failed first send, and with queued messages through retry.

The API accepts `uploads` on session creation, session messages, thread creation
and thread messages. Each upload has a name, base64 data and an optional expected
kind. The server determines the media type from the actual bytes. Clients cannot
provide another conversation's digest or ask the server to fetch an arbitrary URL.

```json
{
  "text": "Inspect this screenshot",
  "agent": "vision-reviewer",
  "uploads": [{"name": "capture.png", "data": "BASE64_PNG_BYTES", "kind": "image"}]
}
```

`POST /api/messages` also accepts image, audio, video, document and file content
blocks with `name` and base64 `data`. A malformed file fails the batch before
earlier messages post. Original bytes are stored under the authenticated tenant;
the transcript and run carry their content digest and metadata. Later turns use
the same attachment references, rather than a different copy of the file.

A model receives only modalities its provider declares it supports. The existing
attachment resolver reports an unreadable or unsupported file explicitly.
Attaching a recording does not configure a transcription service; configure voice
or a reviewed media-processing tool for a model that cannot hear it. Scanned-page
OCR and long video processing likewise need an appropriate model or tool.

Reviewed MCP tools preserve ordered image, audio and embedded document content
through the same typed model boundary. Personal tools still require the person's
reviewed tool list, current consent and any action approval. A model that cannot
consume a tool's returned modality refuses it under saige's conversion policy;
the adapter does not replace the media with an omission marker.

The upload path reuses the deployment's file-blob directory. A deployment with
workers on different hosts must make that existing store available to each worker.
This feature does not create a desktop connection or grant access to local files.
