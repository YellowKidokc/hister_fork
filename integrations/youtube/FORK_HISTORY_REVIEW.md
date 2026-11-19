# Fork history review - 2026-11-19

Reviewed the current master history at deffe927, focused acquisition/search
commits and their source. This is a targeted review, not an exhaustive audit of
every ancestor or of all third-party forks. The current tree adopts Nogon naming.

1. **Reuse the research receipt system.** Fork-specific commit
   [90195d76](https://github.com/YellowKidokc/hister_fork/commit/90195d76e5a2bbb0ecd9d9e2da0e561c3e45add6)
   adds named research jobs, per-file SHA-256 receipts, duplicate detection and
   job-scoped search. Next step: attach transcript artifacts to research jobs,
   preserving the video URL and raw/derived relationship. Already present in
   this fork; do not cherry-pick it again. This integration uses `/api/add`;
   it does not yet create research-job receipts.
2. **Reuse video metadata extraction.**
   [412525cf](https://github.com/YellowKidokc/hister_fork/commit/412525cf4f8911c7f2d7f886974ec5d14d15eca5)
   exposes extractor/semantic settings. The existing yt-dlp extractor supports
   chapters, thumbnails, captions, language, cookies and bounded concurrency.
   Combine those metadata capabilities with this scraper's working transcript
   acquisition, after a live comparison. Do not run two caption fetchers for
   every video by default.
3. **Keep AI retrieval connected to source previews.**
   [b796f837](https://github.com/YellowKidokc/hister_fork/commit/b796f83762f98171bbcf5496f29187ae8ae1227d)
   includes stored-file previews in MCP responses. Useful for future receipt-based
   transcript retrieval; preserve raw timestamps so quotations can lead back to
   the video. Already included in master.
4. **Reuse retrieval controls instead of inventing another search service.**
   MCP pagination, facets, history filters and semantic date/content fixes are
   already in the ancestry (3814fb21, 8d44e7c0, 56ef902c, e2870926).
   Channel/language/source facets and saved research queries are good follow-ups;
   this package only sends the supported `youtube` label today.

Recommendation: first validate download -> Markdown -> search end to end, then
connect acquisition runs to the existing research jobs UI. A scheduled channel
watch list and per-run new/skipped/failed counts would build on that foundation.
