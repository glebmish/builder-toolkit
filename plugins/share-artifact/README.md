# share-artifact

Share a finished image or GIF with the user as a viewable, signed URL.
Uploads the artifact to Cloudinary and hands back a link that works on
mobile, a chat client, or any remote control where a local file path is
useless. Covers session-id-based naming, the authenticated-asset privacy
model, and tag-based cleanup.

Scoped to images and GIFs. It does **not** produce or edit artifacts —
hand it a PNG or GIF that already exists.

## Install

Claude Code:
```
claude plugin marketplace add glebmish/builder-toolkit
claude plugin install share-artifact@builder-toolkit
```

Standalone skill:
```
npx skills add glebmish/builder-toolkit --skill share-artifact
```

**Prerequisites**

- `cld` on `PATH` — the Cloudinary CLI. Install: `pipx install cloudinary-cli`.
- `CLOUDINARY_URL` exported: `cloudinary://<API_KEY>:<API_SECRET>@<CLOUD_NAME>`.
  Verify once with `cld ping` → `{"status": "ok"}`.

## What it does

1. **Verify** the image before upload — Read it and confirm it shows what
   you'll claim about it. Once the URL is in chat, the claim is made.
2. **Upload** to Cloudinary as an `authenticated` asset, named off the
   Claude session id (`<date>-<sid_short>/<NNN>-<slug>`) so every artifact
   from one conversation groups together.
3. **Return** the `secure_url` — a signed link that does not expire by
   default and works without logging in.

## Privacy model

Assets upload as `type=authenticated`: the raw delivery URL returns `401`
without a signature, and the `secure_url` carries that signature in an
`s--XXXX--` fragment. Treat the URL like a password for that one image —
anyone with it can view until the asset is deleted.

## Cleanup

Pruning is manual, by tag:

```
cld admin delete_resources_by_tag <sid>             # one conversation
cld admin delete_resources_by_tag <YYYY-MM-DD>      # one day
cld admin delete_resources_by_tag share-artifact    # everything shared via this skill
```
