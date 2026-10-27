# ADR-0005: Image uploads and thumbnails

- Status: accepted
- Date: 2026-10-27

## Decisions

1. **Dependency `golang.org/x/image` v0.47.0** (BSD-3, part of the Go project). The standard library decodes PNG and JPEG only; WebP decoding and good-quality scaling need it. No other image dependency.
2. **Streamed, validated uploads.** The multipart body is copied to a temp file with a byte limit; extension, magic bytes and pixel count (from the header, via `image.DecodeConfig`) are checked before anything is decoded. Files are stored under generated names (`ab/<random>.png`); the original name is only metadata.
3. **Thumbnails on demand, one at a time.** A mutex allows one decode at a time, results are cached as JPEG (longest edge 320 px) under `uploads/thumbs`. Peak memory for a 20 MP image is measured in `benchmarks/RESULTS.md` (about 85 MB JPEG, 134 MB PNG).
4. **Authorised streaming.** Files are served only through `/api/v1/attachments/{id}[/thumb]` after the same project access check as the item, with `nosniff` and a sandbox CSP. There is no public path to `/uploads`.
5. **No delete yet.** Like work items, attachments are not removed; an item keeps at most 50.

## Consequences

- A 20 MP PNG needs about 135 MB while its thumbnail is generated; the 384 MB container limit holds, but several large uploads viewed at once are serialised.
- Originals are stored untouched (EXIF stays); only thumbnails are re-encoded.
