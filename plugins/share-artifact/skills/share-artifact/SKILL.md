---
name: share-artifact
description: Use when the user needs to see an image or GIF that exists only as a local file — uploads it to Cloudinary and returns a signed URL to paste in chat. For when the user is on mobile, a chat client, or any remote control where a local file path is useless. Takes a finished PNG or GIF; it does not produce or edit artifacts.
---

# Sharing an image or GIF as a viewable URL

You have a finished artifact — a PNG or a GIF — and the user can't open it as a local file. This skill uploads it to Cloudinary and hands back a durable, signed URL you can paste straight into chat.

It works on the artifact as-is. Producing the artifact (rendering a terminal capture to a PNG, recording a GIF) and editing it (annotating arrows, boxes, labels) are upstream concerns owned by other skills — this skill starts from a file that already exists on disk.

## Scope

- **In scope:** images (PNG) and GIFs.
- **Out of scope:** producing the artifact (capture, render, GIF encoding) and editing it (annotation). Hand this skill a finished image.

## Prerequisites

- `cld` on PATH. Install: `pipx install cloudinary-cli`.
- `CLOUDINARY_URL` exported: `cloudinary://<API_KEY>:<API_SECRET>@<CLOUD_NAME>`. Verify once with `cld ping` → `{"status": "ok"}`.

---

## Step 1: verify before uploading

Before running `cld uploader upload`, Read the image with your image tool. Confirm it shows what you intend to claim about it — the right state, the right annotation on the right target, the expected frames in the right order for a GIF.

**Once the URL is in chat, the claim is made.** The user on mobile cannot cheaply re-inspect the artifact; whatever you paste is what they'll reason about. A mislabeled or wrong-content image costs multiple round-trips to correct.

Upload also sends the bytes off-box to Cloudinary, a third-party SaaS. Before uploading, confirm the image contains nothing the user wouldn't want on a third party — secrets, tokens, private PII, an internal UI they didn't mean to share. If in doubt, ask before uploading.

This is `superpowers:verification-before-completion`: evidence before assertions. Skip the verify step only for artifacts whose correctness is already established (e.g. an unmodified capture you're forwarding raw, already verified by the skill that produced it).

---

## Step 2: upload to Cloudinary

### Naming: derive from the Claude session id

Don't invent ad-hoc session labels. Obtain the Claude session id (invoke `get-session-id` if you don't already have it from earlier in the conversation) and derive folder and tag names from it. Using the session id groups every artifact from one conversation together in the Media Library and makes cleanup a single command.

```bash
SID=<session-uuid-from-get-session-id>     # e.g. 439fcd07-ece6-4898-b23b-df5009f3d0f3
SID_SHORT=${SID:0:8}                       # 439fcd07 — short folder segment
```

Cloudinary `public_id`:

```
<YYYY-MM-DD>-<SID_SHORT>/<NNN>-<slug>
```

- `<YYYY-MM-DD>` — today's date in UTC. Use `date -u +%Y-%m-%d`. Useful for date-based cleanup tags.
- `<SID_SHORT>` — first 8 chars of the Claude session id. Groups every artifact from one conversation together in the Media Library.
- `<NNN>` — 3-digit zero-padded counter, starting at `001`, incremented per upload within the session.
- `<slug>` — short kebab-case description of what this specific shot shows (e.g. `initial-render`, `after-j-x8`, `help-modal`).

Tags on every upload: `<YYYY-MM-DD>,<SID_SHORT>,$SID,share-artifact`. Include both short and full session id — short for quick folder-matching, full for precise lookup later.

### Determining the next counter

```bash
FOLDER="$(date -u +%Y-%m-%d)-$SID_SHORT"
COUNT=$(cld --verbosity ERROR admin resources type=authenticated prefix="$FOLDER/" max_results=500 2>/dev/null \
        | jq -r '.resources | length')
NEXT=$(printf '%03d' $((COUNT + 1)))
```

`max_results=500` is the Admin API's per-call ceiling. For a burst of uploads — or any folder past 500 assets — cache the count in a shell variable and increment locally rather than re-querying, so the counter can't undercount and collide.

### The upload command

```bash
LOCAL_PATH=<local-path>                 # the existing PNG/GIF on disk
# Derive the slug through a sanitizer rather than hand-substituting it into the
# command — this is what guarantees the [a-z0-9-] constraint and stops any shell
# metacharacter in the description from reaching the shell.
slug=$(printf '%s' "<short description of this shot>" \
        | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9-' '-' \
        | sed 's/-\{2,\}/-/g; s/^-//; s/-$//')

URL=$(cld --verbosity ERROR uploader upload "$LOCAL_PATH" \
        public_id="$FOLDER/$NEXT-$slug" \
        asset_folder="$FOLDER" \
        type=authenticated \
        tags="$(date -u +%Y-%m-%d),$SID_SHORT,$SID,share-artifact" \
      | jq -r '.secure_url')
echo "$URL"
```

Why two folder params:

- `public_id=<FOLDER>/<NNN>-<slug>` — unique ID + URL path. Slashes produce a folder-like URL structure (`.../<FOLDER>/<NNN>-<slug>.png`).
- `asset_folder=<FOLDER>` — the Media Library browser's folder. Without this, assets file at the root in the console even if `public_id` has slashes.

Set both to the same value.

Paste `$URL` in chat. Done.

---

## Privacy model

- Assets upload as `type=authenticated`. Direct `res.cloudinary.com/.../authenticated/...` URLs without a signature return **401**.
- The `secure_url` you get back includes a signature fragment (`s--XXXXXXXX--`). That signature is what grants access.
- The URL does **not** expire by default.
- Treat the URL like a password for that specific image: anyone with it can view until the image is deleted. Don't forward.
- Works without logging in — signature is URL-based, not cookie-based.
- Because it doesn't expire, the signed URL stays live wherever the chat transcript ends up (logs, backups, sync). For anything sensitive, prune at the end of the conversation with the tag-based cleanup below rather than leaving it reachable indefinitely.

## Cleanup

Automatic cleanup is out of scope. When you want to prune, use tags:

```bash
cld admin delete_resources_by_tag $SID_SHORT        # one conversation (short id)
cld admin delete_resources_by_tag $SID              # one conversation (full id — more precise)
cld admin delete_resources_by_tag <YYYY-MM-DD>      # one day
cld admin delete_resources_by_tag share-artifact    # everything shared via this skill
```

## Pitfalls

- **Forgetting `type=authenticated`** — uploads as public. Anyone who guesses the `public_id` can view. Always set `type=authenticated`.
- **Reusing `public_id` across uploads** — replaces the previous asset. Always bump the counter.
- **Spaces or odd chars in the slug** — pass the description through the sanitizer in Step 2 instead of hand-writing the slug into the command; it forces `[a-z0-9-]` and strips anything the shell could misread.
- **`cld ping` failing** — `CLOUDINARY_URL` not exported or malformed. Confirm it's present with `[ -n "$CLOUDINARY_URL" ]`. Don't run `cld config` or echo `CLOUDINARY_URL` into chat — both print the API key and secret.
- **Free-plan quota** — 25 GB. Captures are tiny; `cld admin usage` shows current totals.
- **Very tall PNGs** (hundreds of lines of scrollback rendered to one image) display awkwardly on mobile chat. Cap the artifact's height upstream, where it's produced.
- **Uploading something other than an image or GIF** — out of scope. This skill assumes a viewable image; for other artifact types it has no opinion on naming or privacy.
