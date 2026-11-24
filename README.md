# mplay · Your media, anywhere

**English** | [简体中文](README.zh-CN.md)

[![ci](https://github.com/hktalent/mplay/actions/workflows/ci.yml/badge.svg)](https://github.com/hktalent/mplay/actions/workflows/ci.yml)
[![release](https://github.com/hktalent/mplay/actions/workflows/release.yml/badge.svg)](https://github.com/hktalent/mplay/actions/workflows/release.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
<img width="800" alt="image" src="https://github.com/user-attachments/assets/0f73b802-a12e-4760-b381-23d5f1f23710" />

Download builds: [Releases](https://github.com/hktalent/mplay/releases) — macOS universal (Intel + Apple Silicon), Linux x64, Windows x64. Unpack and run.

A local-first audio/video player: scan your media folders → match lyrics and subtitles from filenames and tags → keep them in sync line by line during playback.
**One frontend codebase serves both the website (m.51pwn.com) and the macOS / Linux / Windows desktop bundle (urlbox shell).**

```
┌──────────────┐   HTTP(loopback) ┌─────────────────────────────┐
│ urlbox window│ ───────────────▶ │  mplay single binary (Go)    │
│ or browser   │◀──── same UI ─── │  go:embed web/ · zero CGO    │
└──────────────┘                  │  library index / ID3·FLAC·MP4│
                                  │  LRC·SRT·VTT·ASS parsing     │
                                  │  LRCLIB·Kugou·NetEase(opt)   │
                                  │  OpenSubtitles               │
                                  └─────────────────────────────┘
```

## Capabilities

**Playback**
- Local audio and video: MP3/FLAC/M4A/AAC/OGG/OPUS/WAV/APE… and MP4/MKV/WEBM/MOV/AVI…
- HTTP Range streaming (scrubbing video, play-while-loading), cover art read from tags and cached
- Play queue, previous/next, ±10s, speed, volume, fullscreen, mini player bar
- Volume lives on the same row as the progress bar (mute button + percentage), adjustable at any width; `M` to mute, `↑/↓` in 5% steps
- **Current lyric line always visible** just above the progress bar: the line being sung (plus translation), with dedicated text for intro/interlude and for instrumental tracks; on the library page the mini bar uses that line instead of the artist name
- **The minimized (mini) bar is a full player**: after you switch to library/search, the bar floating at the bottom has a volume slider, previous, play/pause, next, plus a 12-bar mini spectrum; clicking the cover or title returns to the player page. Skipping tracks from the mini bar does not yank you back to the player page.
- **Audio visualization**: a spectrum strip under the player area driven by WebAudio `AnalyserNode` (48 bars, low frequencies get more bins, peaks decay smoothly); while playing, the cover, type size and background glow breathe gently
- Drag a local file into the page to play it (not written to the library; a sibling `.lrc/.srt` is picked up too)

**Lyrics / subtitles**
- Match order: same-name lyric/subtitle file → embedded tags (ID3 USLT/SYLT, FLAC, MP4) → local cache → LRCLIB → Kugou → NetEase (off by default) → same-title borrowing → (video) OpenSubtitles
- Instrumental tracks are marked, not force-matched: when the lyric library says there is no lyric (Kugou returns "instrumental, enjoy"), the track is marked "no lyrics, instrumental", excluded from lyric counts and never retried in a loop
- Parses LRC (including `[offset:]`, multiple timestamps, enhanced per-word `<mm:ss.xx>`), SRT, VTT, ASS/SSA
- Bilingual merge: same-timestamp lines / two files with complementary languages / two-line SRT → primary line + translation line
- Encoding detection: UTF-8 BOM / UTF-16 / strict UTF-8 / GB18030 fallback (legacy Chinese lyrics do not turn into mojibake)
- Per-word highlight, click-to-seek, ±0.1s manual sync, large-type mode, export LRC, manual import
- Video subtitles rendered over the picture (Chinese primary line + translation line), font size follows the window

**Search**
- Online song search: Apple previews (official 30s), Internet Archive (public domain / CC, full playback), Jamendo (optional), **custom source plugin** (one line of JSON to plug in your own API)
- Reverse lookup by a lyric fragment: local full-text lyric index (jumps straight to that timestamp) + NetEase lyric search (metadata only) + lyric-library title matching

**Library**
- Four headline numbers: **all / audio / with lyrics / missing lyrics** — click one to filter (definitions in "The four library numbers" below)
- Toolbar "Open local folder": on a machine with a GUI it pops the **native directory picker** (macOS `choose folder` / Linux zenity·kdialog / Windows FolderBrowserDialog); the chosen folder is added to `mediaDirs` and scanned immediately, existing folders are not added twice
- Headless web deployments (systemd / containers / remote) fall back to the browser's directory picker: audio/video inside that folder are pushed into the play queue **temporarily**, not into the library (a browser cannot hand over real paths, so it cannot scan your disk for you)
- A single file can still be dragged into the window to play, without touching the library

**Page**
- Header / footer are `position: fixed` and always visible; body and overlays reserve space via `--header-h / --footer-h` in real time
- Footer links come from the single `site-links` data source (`python3 site-links/sync.py` generates it; never hand-edit `web/footer.js`)
- Mobile: at 390px the header splits into two rows and the player bar wraps

## Quick start

### Web / server

```bash
cd /Users/51pwn/MyWork/mplay
./scripts/build.sh                 # produces bin/mplay (frontend already embedded)
./bin/mplay -media ~/Music -open    # defaults to 127.0.0.1:8737
```

Then open `http://127.0.0.1:8737`. In "Settings" you can add media folders, toggle lyric sources, set an OpenSubtitles key, define custom sources and an access token.

Server deployment (public-facing):

```bash
./bin/mplay -addr :8737 -media /srv/media
# or systemd / Docker, see packaging/
```

### Desktop

```bash
./scripts/desktop.sh               # = build + start server locally + open the urlbox window
```

- Desktop mode binds the loopback address only, and steps to the next port if one is taken (up to 10)
- urlbox lookup order: `MPLAY_URLBOX` → PATH → next to the binary → `~/MyWork/urlbox/target/release` → `/usr/local/bin`, `/opt/homebrew/bin`
- If urlbox is missing it falls back to the system browser instead of failing to start
- Desktop adds three abilities: **reveal in Finder/Explorer**, **open in the system player**, and system-level fullscreen

### Packaging for three platforms

```bash
./scripts/package.sh               # server/web build: dist/mplay-<ver>-{os}-{arch}.* (5 targets)
./scripts/package-desktop.sh       # desktop build: mplay + urlbox + double-click launcher; macOS also gets MPlay.app
```

- The server package is **a single executable** (frontend embedded): double-click `mplay.exe` on Windows, `./mplay -open` on macOS/Linux
- The desktop package needs `urlbox` first: `MPLAY_URLBOX=/path/to/urlbox ./scripts/package-desktop.sh`
- Desktop bundle layout: `bin/mplay` + `bin/urlbox` + `start-mplay.{sh,command,cmd}` + `使用说明.txt`; macOS additionally gets an ad-hoc signed `MPlay.app`

### Automated releases (GitHub Actions)

`.github/workflows/release.yml` — push a tag and it builds the three desktop bundles plus the server packages and publishes a Release:

```bash
git tag v1.0.1 && git push origin v1.0.1
```

| Artifact | Contents |
| --- | --- |
| `mplay-desktop-<ver>-macos-universal.zip` | contains `MPlay.app` (mplay + urlbox lipo'd into a universal binary) |
| `mplay-desktop-<ver>-linux-x64.tar.gz` | `./start-mplay.sh` |
| `mplay-desktop-<ver>-windows-x64.zip` | `start-mplay.cmd` |
| `mplay-<ver>-{darwin,linux}-{amd64,arm64}.tar.gz`, `mplay-<ver>-windows-amd64.zip` | server/web single file |
| `SHA256SUMS` | checksums |

CI builds `urlbox` with `cargo build` on the spot (Linux installs the webkit2gtk dependencies), so the window shell in all three desktop bundles matches upstream.
Manual run: Actions → release → Run workflow (version number optional, draft optional).

`.github/workflows/ci.yml` — on every push: `gofmt` check, `go vet`, `go build`, frontend `node --check`, script `bash -n`, then boots the server once for an HTTP smoke test.

## Supported environments

### Runtime

| Form | Platforms | Requirements |
| --- | --- | --- |
| Server / web | macOS (Intel / Apple Silicon), Linux amd64 / arm64, Windows amd64 | **Single file, zero CGO**: no Go runtime, no database; unpack and run |
| Desktop | macOS universal bundle (`MPlay.app`), Linux x64, Windows x64 | "Open local folder" uses the native dialog: macOS `osascript`, Linux `zenity` or `kdialog`, Windows PowerShell; without a GUI session the frontend falls back to the browser picker. Also needs a `urlbox` window shell: Linux requires system `webkit2gtk` (wry dependency, install `libwebkit2gtk-4.1-0` on Debian/Ubuntu), Windows requires the WebView2 runtime (usually present on Win10/11); macOS builds are ad-hoc signed, so the first launch needs right-click → "Open" |
| Browser access | Modern Chrome / Edge / Firefox / Safari | Playback relies on the browser decoder: `.mkv/.avi/.wmv` mostly will not play, prefer `.mp4/.webm` on the web; the desktop build can fall back to "open in system player" |
| Mobile | Phone browser hitting the same address | At 390px the header splits into two rows and the player bar wraps; protect it with `MPLAY_TOKEN` when exposed via `-lan` or a reverse proxy |

### Build environment (when compiling yourself)

- **Go 1.26+** (`go.mod` declares `go 1.26.0`), cross-compile with `CGO_ENABLED=0`; `./scripts/build.sh` produces a binary for the current platform in one command.
- Packaging scripts: `bash` + `zip`/`tar`; the desktop bundle additionally needs a **Rust toolchain** (`cargo build --release` for `urlbox`).
- Zero frontend build: `web/` is plain HTML/CSS/JS embedded through `go:embed`; CI only runs `node --check` for syntax (Node 20+).
- Regenerating the footer needs `python3` (`site-links/sync.py`).

### Network and privacy

- Lyric matching talks to `lrclib.net`, `songsearch.kugou.com` / `krcs.kugou.com` / `lyrics.kugou.com`; all traffic honours the system proxy (`HTTP(S)_PROXY`). **Fully offline still works**: same-name lyrics, embedded lyrics, cached lyrics and decided conclusions all live locally.
- No audio upload by default: only title/artist/duration/filename leave the machine. With `aiUploadAudio` enabled, the first 6 MB of a clip is uploaded only when *you* manually identify one track.
- Data stays local: config in `~/.config/mplay/config.json`, cache in `~/.cache/mplay` (`lyrics/`, `subtitles/`, `covers/`, `library.json`). No cloud account, no telemetry.

## Flags and environment variables

| Flag | Description |
| --- | --- |
| `-addr` | Listen address, `127.0.0.1:8737` or `:8737` |
| `-media` | Media folder, repeatable |
| `-desktop` | Desktop mode (loopback only + launch urlbox) |
| `-open` | Open in the system browser after start |
| `-lan` | Allow LAN access |
| `-config` / `-cache` | Config file / cache directory |
| `-no-scan` | Skip the initial scan |
| `-version` | Print version |

Environment: `MPLAY_ADDR`, `MPLAY_MEDIA` (`:`-separated), `MPLAY_LAN`, `MPLAY_JAMENDO_CLIENT_ID`, `MPLAY_OS_API_KEY`, `MPLAY_TOKEN`, `MPLAY_URLBOX`, `MPLAY_TRACE=1` (debug log).

Config file: `~/.config/mplay/config.json`; cache: `~/.cache/mplay` (`lyrics/`, `subtitles/`, `covers/`, `library.json`).

Lyric-related settings:

| Field | Default | Description |
| --- | --- | --- |
| `enableLRCLIB` | true | LRCLIB source toggle (Settings → "Lyrics & subtitles") |
| `enableKugou` | true | Kugou source toggle (best coverage for Chinese internet singers and covers) |
| `enableNeteaseSource` | false | NetEase source toggle (easily 405-rate-limited per IP, off by default) |
| `autoCacheLyrics` | true | Cache lyrics in the background after a scan |
| `writeSidecar` | false | Write a same-name `.lrc` next to the media file on a hit (by default only the cache is written, your folders are untouched) |

## HTTP API (the frontend is its first client)

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/health`, `/api/version` | Health check, version |
| GET/POST | `/api/config` | Read/write config (secrets are returned as booleans only) |
| GET | `/api/library?q=&kind=&filter=&sort=&limit=` | Library; `filter=lyric|nolyric|pending|instrumental`, `kind=audio|video`; an entry's `noLyric` means "confirmed no lyrics (instrumental)" |
| POST | `/api/library/scan` | Rescan |
| POST | `/api/track/{id}/identify` | Identify one track (body `apply/audio/fetchLyrics`) |
| POST | `/api/library/identify` | Batch-identify unmatched tracks (body `limit/onlyMissing/apply`) |
| POST | `/api/ai/check` | Probe whether the AI endpoint works |
| GET | `/api/track/{id}`, `/api/stats` | Track detail, statistics (`total/audio/video/audioWithLyric/withLyric/noLyric/pending/bytes`) |
| GET | `/api/lyric/{id}?force=1&pick=` | Fetch lyrics (auto-parsed by priority); instrumental returns `{doc:{instrumental:true,note:"无歌词，纯音乐",cues:[]}}` |
| POST | `/api/lyric/{id}?ext=.lrc` | Import raw text (body is plain text) |
| GET | `/api/lyric/{id}/candidates` | LRCLIB candidates |
| GET | `/api/remote/lyric?track=&artist=&album=&duration=&pick=` | Online lyrics (no local track needed) |
| GET | `/api/search?q=&limit=` | Online song search |
| GET | `/api/search/lyrics?q=` | Reverse lookup by lyric fragment (local / remote / library) |
| GET | `/api/sources` | Current sources and their state |
| POST | `/api/pick/folder` | Native folder picker, returns `{ok,path}` (or `{ok:false,cancelled:true}`); local direct access only |
| POST | `/api/open` | `{"id":..,"action":"reveal"|"player"}` (desktop only) |
| GET | `/media/{id}`, `/cover/{id}` | Media stream (Range), cover art |

With `authToken` set, `/api`, `/media` and `/cover` all require the `X-Mplay-Token` header, `?token=`, or the `mplay_token` cookie.

**With no token (since v1.4.1)**: only requests that are *local direct connections* are allowed; anything arriving through a reverse proxy or from the internet gets `403`. The test is the request's origin, not the listen address: the peer must be loopback, no forwarding headers may be present (`X-Forwarded-For`/`X-Real-IP`/`Forwarded`…), and the browser must not mark the request `Sec-Fetch-Site: cross-site`. The reason is concrete — bind `127.0.0.1` and put an HTTPS reverse proxy in front, and `RemoteAddr` is forever `127.0.0.1`; judging by listen address alone treats the whole internet as "local". Details in "Security model" below.

## Track title identification (local rules + optional AI)

Files from media sites are often called `01. site - artist - title [320k].mp3`, sometimes `abc1234.mp3`, with no tags at all. Identification runs in three stages, **each more expensive than the last, so it short-circuits in order**:

1. **Local candidates**: embedded tags → filename (forward + reversed + de-bracketed/de-suffixed) → first line of an existing lyric. Zero cost, works offline.
2. **Lyric-library validation**: search candidates on LRCLIB and score them as "title 0.62 / artist 0.23 / duration 0.15"; only ≥0.62 counts as a hit, and the **library-confirmed title** is written back to the library (this stage does not need AI).
3. **AI fallback**: only when the first two stages are unsure (no hit or confidence <0.8) is the model asked. The model's answer **must still pass lyric-library validation**:
   - Batch identification (`POST /api/library/identify`) writes back only validated results; unsure entries get suggestions in the response and are not persisted;
   - Single-track identification (`POST /api/track/{id}/identify`) writes back when validation passes or model confidence ≥0.75, otherwise returns `apply:false` with "not confident, not written back: closest is …", so noise never overwrites the original filename clue.

### Configuration (Settings → "AI metadata identification (optional)")

| Field | Description |
| --- | --- |
| `aiIdentify` | Master switch. When off, local rules + lyric library still work normally |
| `aiBaseUrl` | OpenAI-compatible endpoint, e.g. Doubao Ark `https://ark.cn-beijing.volces.com/api/v3`, `https://api.openai.com/v1`; self-hosted vLLM/Ollama also take `/v1` |
| `aiModel` | Model / inference endpoint ID, e.g. `doubao-seed-1-6` |
| `aiApiKey` | Key (after saving, the API only returns the `aiConfigured` boolean, never the plaintext) |
| `aiUploadAudio` | Allow sending an audio clip to the model. Off by default; when on, only a **manually triggered single-track identification** uploads, batch identification is always text-only |

- Entry points: library toolbar "Identify unmatched" (batch; by default only audio missing a title/artist, max 20 per run, 90s total); player lyric toolbar "Identify" (single track, may include an audio clip); Settings "Test connection".
- Audio upload details: only the first 6 MB of the file, `m4a/mp4` sent as `mp4`, everything else by extension, via `input_audio` (base64); on failure it degrades to text-only identification.
- Privacy boundary: without `aiUploadAudio`, only the filename, duration and the first few lyric lines leave the machine; with it, a clip is uploaded only when you manually identify a single track.
- Fully usable without AI: the local-rules + LRCLIB-validation path involves no external model.

## Lyric sources and multi-candidate matching (v1.3)

Lyric matching has three layers: **filename/tag cleanup → multi-candidate arbitration → multi-source fallback**. The principle is unchanged — better a track with no lyrics than a track with wrong lyrics.

### Three lyric sources (in order, individually switchable)

| Source | Default | Description |
| --- | --- | --- |
| LRCLIB | on | Global community library, best coverage for Western and mainstream Chinese music, timed lyrics; `/api/search` to query, `/api/get` for exact matches |
| Kugou | on | Best for Chinese internet singers, covers, folk/ethnic instrumental music; fetch path is "search → lyric candidates → download base64 LRC"; rate limit 900ms |
| NetEase | **off** | The API works but is rate-limited per IP; in testing, dense request bursts produce persistent 405 "too frequent" and no cooling-off, so it ships off with the code retained (`enableNeteaseSource`) |

Same-title borrowing comes last: search by title only, require `title ≥0.92` and (`artist ≥0.45` or duration delta ≤3s) before borrowing, so an ethnic-music "无问" never gets Mao Buyi's lyrics.

### How to use

- Settings → "Lyrics & subtitles" card: `enableLRCLIB`, `enableKugou`, `enableNeteaseSource` toggles take effect on save and are pushed to the background cache worker.
- Or edit the config directly: the same three boolean fields in `~/.config/mplay/config.json`.
- API: `GET /api/config` reads the current state, `POST /api/config` takes partial updates such as `{"enableKugou":false}`; `GET /api/sources` shows the current source state.

### Matching details (all learned on a real library)

1. **Cleanup** (`internal/media/guess.go`): drop the extension, site prefixes, bracketed noise (bitrate / format / resolution / `live` / `remix` / full version / accompaniment / "double bitrate" / master / Dolby / test disc / audiophile / DJ version / Douyin / Kuaishou / site names…), plus bare noise tokens outside brackets; `01.` numbering supports 1–4 digits, both `037刘至佳` (number glued to Han characters) and `067LBI利比` (number + letters + Han) are split, while `9420-麦小兜` keeps its 9420; a bare `-` is split only when "one side is CJK text, or both sides are multi-word" (`All Night-少女时代` splits, `Taylor-Swift` does not).
2. **Tags cannot be trusted blindly**: bulk transcoding by media sites often writes garbage tags (`Love Story-Taylor` / `Swift`, `kuwo` / `kuwo`, `8.潇湘水云`, `2、相思（live）`, `artist=小高网分享xgw5.com`). Tag values now go through site-word filtering + `CleanName`, then are used **as the first-ranked candidate only**, with filename candidates queued behind them for the lyric library to arbitrate.
3. **Version suffixes are not a different song**: `coreTitle` strips bracketed attachments such as `(Live)`/`(DJ版)`/`(女版)`/`(古琴名曲)`/`(翻奏)` before comparison, so `做你的爱人` and `做你的爱人 (DJ 慢摇版)` are not treated as two songs, and duration then rejects the wrong version (in testing it picked the 285s original, not the 194s slow-mix).
4. **Artist spelling normalization**: `normArtistToken` strips brackets and numeric prefixes like `003`, and treats `& / , 、 ; | x vs feat ft with -` as separators, taking the best score — `003L（桃籽）` matches Kugou's `L（桃籽）、周林枫、三楠`.
5. **Double threshold + reversed-order ambiguity**: a hit needs `title ≥0.75` and `artist ≥0.55`; if both A/B and B/A orders exist among candidates, a record that only supports the non-preferred direction and is the sole one is rejected (one community upload does not prove who is who); reversed dirty entries are dropped outright.
6. **Request economy**: at most 4 candidates, 3 concurrent within a batch, 6 globally, LRCLIB throttled at 600ms, 15s per-request timeout; only a single hit triggers one keyword-search top-up, zero hits trigger none.
7. **Write-back**: confirmed spellings go into the library index only (`metaSource=lrclib`); **audio files and tags are never modified**.

### Measured (584-track real library)

| Metric | Result |
| --- | --- |
| With lyrics | 498 / 584 (85.3%) |
| Instrumental (marked as no lyrics) | 48 |
| After excluding instrumental/sfx/very short clips | 498 / 511 (97.5%) |
| Surface hit rate (counting instrumental placeholders, the pre-v1.3 metric) | 546 / 584 (93.5%) |

The 38 leftovers: 19 instrumental/folk/sfx, 6 very short or untagged clips, 13 covers that neither library actually has.

An earlier focused test on 28 extremely dirty filenames hit **27/28 canonical records** (all with title similarity 1.00), correcting all 4 previous "hit but wrong entry" cases; the single miss was LRCLIB rate limiting and matched when retried individually.

## Instrumental tracks: marked, not force-matched

For instrumental pieces the lyric library often returns a "lyric" whose content is literally `纯音乐，请欣赏` ("instrumental, please enjoy"). Such placeholder text used to count as a hit, producing a fake lyric line in the UI and inflated statistics.

Current handling:

- **Detection**: strip LRC time tags and `[ti:]/[total:]/[qq:]` metadata first, then test for fixed phrasings such as `纯音乐+欣赏`, `纯音乐`, `暂无歌词`/`无歌词`; a real lyric that happens to contain "纯音乐" is not misclassified (locked by a test case).
- **Persistence**: `<cache>/lyrics/<id>.nolyric.json` (`reason=instrumental`, `note=无歌词，纯音乐`), and the corresponding fake lyric cache is deleted.
- **Library**: entries gain a `noLyric` field (mutually exclusive with `lyricSource`, **not counted as "with lyrics"**), the list badge shows "no lyrics, instrumental", and the detail page states it instead of "lyrics not found".
- **No repeat asking**: the pending-cache list skips these entries — which also fixes the old "permanent miss retried forever every round" behaviour.
- **Automatic migration of old data**: on startup the cache is scanned and stored placeholder lyrics are converted into conclusions (48 entries corrected in one run during testing).

To overturn a conclusion: delete `<cache>/lyrics/<id>.nolyric.json`, then force a re-match with `GET /api/lyric/{id}?force=1`.



## The four library numbers and the player UI (v1.4)

| Number | Definition | Clicking it means |
| --- | --- | --- |
| All | Total audio + video in the library | no filter |
| Audio | Entries with `kind=audio` | `kind=audio` |
| With lyrics | Audio entries that have lyrics (instrumental conclusions do not count) | `filter=lyric` |
| Missing lyrics | Audio entries with no lyrics and no instrumental conclusion yet | `filter=pending` |

`GET /api/stats` and the `stats` object in `/api/library` carry: `total / audio / video / audioWithLyric / withLyric / noLyric / pending / bytes`. `withLyric` counts video subtitles too, `audioWithLyric` counts audio only — the UI's "with lyrics" uses the latter.
The toolbar also has two supplementary filters: `Instrumental` = `filter=instrumental` (entries with a `noLyric` conclusion), `All missing` = `filter=nolyric` (no `lyricSource`, instrumental included).

**Player**

- **Volume always there**: the slider sits on the same row as the progress bar, followed by the mute button and percentage; `M` mutes, `↑/↓` step 5%, double-clicking the slider resets to 80%. Even at 390px it is not hidden — only the slider narrows to 88px and the percentage text disappears.
- **Current lyric line**: a fixed row above the progress bar showing the line being sung plus its translation; intro/interlude shows `♪ 前奏 / 间奏…`, instrumental shows the conclusion, no lyrics shows "no lyrics". On the library page the mini bar uses that line in place of the artist name.
- **Minimized (mini bar)**: floats at the bottom once you leave the player page; volume, previous/play/next, plus a 12-bar mini spectrum (sharing the same spectrum data as the player page, no extra analysis); skipping from the mini bar does not jump back to the player page. At 390px the full button set stays, only the spectrum is dropped and the slider narrows.
- **Audio visualization**: a spectrum strip between the player area and the player bar (48 bars, low frequencies get more bins, peaks decay smoothly), with the cover/type/background glow breathing gently during playback. The real spectrum comes from WebAudio `AnalyserNode`; to avoid sacrificing playback compatibility, **the main playback element is not bound into WebAudio** (a cross-origin source would be muted if bound), instead a hidden `<audio>` pulls the same stream for analysis with gain 0 (no second audible copy). So the local library yields a real spectrum, while cross-origin online sources / browsers without WebAudio / 5 consecutive seconds without signal fall back to a decorative animation (still following play/pause). The strip is hidden during video playback.

## Background lyric caching

After a scan you no longer have to open each track in the player to fetch its lyrics:

- A successful scan starts background caching (`Library.OnScanned` hook); a round also runs at startup; toggling settings triggers it immediately too.
- **Asynchronous, concurrency-limited**: a dedicated worker (4 concurrent + LRCLIB 600ms / Kugou 900ms throttling, 429/503 backoff), 120s per-track timeout, using the same parsing path as playback — it never competes with playback requests or affects listening.
- **Only fills gaps**: handles only audio entries with no lyrics, no embedded subtitles, no cache hit and no instrumental conclusion; everything else is skipped.
- **Shows progress while caching**: each hit immediately updates the library (lyric badge + duration/title corrections); the toolbar badge is a **ring + percentage + `3/12`**, with the full hit/miss/skip breakdown on hover; on completion it shows a green full ring and hit rate, collapsing after 4 seconds.
- **Failures retry, but not forever**: rate-limited or network failures stay in the queue and are retried on the next heartbeat (every 15s); entries the lyric library confirms as "instrumental/no lyrics" get a conclusion and leave the queue.

| Setting / API | Description |
| --- | --- |
| `autoCacheLyrics` | Default **true**. Settings → "Automatic lyric caching" |
| `enableKugou` / `enableNeteaseSource` | Kugou default **true**, NetEase default **false**; two toggles in Settings → "Lyrics & subtitles" |
| `GET /api/cache/status` | `{enabled,lrclib,kugou,pending,audio,withLyric,progress:{running,total,done,hit,miss,skipped,...}}`; `skipped` = already concluded instrumental/no-lyrics |
| `POST /api/cache/run` | Cache lyrics for tracks missing them right away; returns `{already:true,pending}` if already running, or a `hint` when there is nothing to cache |

## Security model (v1.4.1)

Someone asked, point blank: "If I open m.51pwn.com from the internet, can it read non-audio files on my Mac?" So the whole external boundary was re-audited against that question — black-box reproduction first, then code changes, then re-testing. Conclusions and measures are below, with the reproducible parts kept as verification methods.

**Conclusion first: no arbitrary file read was found, but there were several genuinely exploitable privilege gaps — all now closed.**

- **Path traversal: does not exist.** The `{id}` in `/media/{id}`, `/cover/{id}` and `/api/lyric/{id}` is a track hash computed at scan time; `Lib.Get(id)` resolves an index entry first, then `os.Open(entry.Path)` runs. A request string is never concatenated into a filesystem path. Static assets go through `go:embed` + `path.Clean` + rejecting `..`. Measured with `--path-as-is` against `/media/../../../../etc/passwd` and friends: `mux` first 307-redirects to `/etc/passwd`, which lands on the static handler and 404s — **no file content is ever returned**. The `ext` parameter of lyric upload only affects parsing; the file always lands at `<cache>/lyrics/<id>.json`.
- **The scanner only accepts audio/video extensions.** Inside `WalkDir` only `audioExts`/`videoExts` produce index entries (`.mp3 .flac .m4a … .mp4 .mkv …`); everything else never becomes an entry, so the "add one entry to the library" route cannot read `.txt`/`.json`/key files either. Same-name sidecars only read subtitle/lyric files such as `.lrc/.srt/.vtt/.ass`.
- **The real risk was "change the config", not "traversal".** On old versions with no token behind a reverse proxy, anyone could `POST /api/config` and repoint `mediaDirs` at `/Users/<you>` or even `/`, trigger a rescan, get every audio/video file on the disk indexed, then download them one by one through `/media/{id}` (private recordings and home videos included); `/api/library`, `/api/track` and `/api/config` also handed out absolute paths, the config path and the library inventory. Now: public/proxied requests get 403 by default, and for non-local direct connections `path`/`dir`/`configPath` are blanked while `mediaDirs` returns folder names only.
- **SSRF is closed.** The custom source `searchUrl` and the AI endpoint are both server-initiated outbound requests; the old version could point them at any port on `127.0.0.1`, and error strings carried the first 256 bytes of the response body (a read primitive — the measured `ssrf: HTTP 404 404 page not found` was the internal endpoint's own text). Now the URL is validated when the config is written and again before each request: only `http/https`, and any address resolving into private/loopback/link-local/`169.254.169.254` ranges is rejected.
- **GUI actions are judged by request origin.** "Open folder" dialogs and "open in the system player" used to check only whether `Cfg.Addr` was loopback — which, behind a reverse proxy, meant open to the whole world. Now the request itself must be a local direct connection.
- **CSRF.** Write methods require `Origin` to match this site; a third-party page POSTing to the local API through a browser gets 403.
- **Health check no longer leaks paths.** `/api/health` stays public (for monitoring) but now returns the *count* of media folders instead of their absolute paths.

Two operational habits are **still recommended**: always set `authToken` for public deployments (`MPLAY_TOKEN` or the settings page); and when the token travels as `?token=` in a URL, avoid pasting token-bearing links into chats (even though the page sets `Referrer-Policy: no-referrer`, the upstream nginx access log still records the full URL).

## Deploying to m.51pwn.com

```bash
./scripts/build.sh
sudo install -Dm755 bin/mplay /usr/local/bin/mplay
sudo useradd -r -s /sbin/nologin mplay
sudo mkdir -p /srv/media /srv/mplay/cache && sudo chown -R mplay:mplay /srv/mplay
sudo cp packaging/mplay.service /etc/systemd/system/ && sudo systemctl enable --now mplay
sudo cp packaging/nginx-m.51pwn.com.conf /etc/nginx/conf.d/m.51pwn.com.conf && sudo nginx -s reload
```

- Nginx disables `proxy_buffering` and relaxes timeouts for `/media`, so scrubbing is unaffected
- For public exposure also set `MPLAY_TOKEN` (write it into `/etc/mplay/env`)
- Container: `docker build -f packaging/Dockerfile -t mplay . && docker run -p 8737:8737 -v ~/Music:/media mplay`

## Maintenance

- **Footer**: the single source of truth is `../site-links/links.json`; after editing run `python3 ../site-links/sync.py --only mplay`. `web/footer.js` is generated — do not hand-edit it; the `?v=` in `web/index.html` is synchronized automatically.
- **Code layout**:
  - `main.go` startup/listen/signals; `webembed.go` embeds the frontend
  - `internal/config` config and environment variables; `internal/media` scanning, tags, filename parsing, covers, Range
  - `internal/lyrics` LRC/SRT/VTT/ASS parsing, encodings, LRCLIB / Kugou / NetEase sources, instrumental conclusions, subtitle service, fragment index
  - `internal/music` online source aggregation, NetEase lyric search, custom sources
  - `internal/identify` title identification: local candidate generation + LRCLIB scoring + OpenAI-compatible client
  - `internal/netguard` request-origin and outbound-URL checks (local direct access, SSRF guards)
  - `internal/server` routes and API; `internal/desktop` urlbox/browser/system player
  - `web/` frontend: `index.html` structure, `style.css` fixed header/footer and player, `lyrics.js` parsing + sync, `player.js` playback engine, `app.js` routing and business logic

## Known limitations (measured, not guessed)

- When the browser engine cannot decode a container (`.mkv/.avi/.wmv` and similar), playback fails; the UI says so and offers "open in system player" (desktop mode). On the web you have to switch to `.mp4/.webm`.
- **Playable** online sources are only two kinds: official 30-second previews (Apple) and public domain / CC audio (Internet Archive, Jamendo, custom sources). The project ships no audio URLs for any third-party music platform.
- LRCLIB matches by title/artist only (it does not search lyric text), so "reverse lookup by lyric fragment" relies on the **local full-text index + NetEase metadata search**; a NetEase hit only yields title/artist/home page, never audio.
- The lyric fragment index covers the **local library only** (including already-cached remote lyrics); new lyrics pulled by an online preview do not enter the index.
- Internet Archive's search endpoint occasionally times out from mainland China (8s); failures are surfaced as "some sources failed".
- Lyric coverage is bounded by the lyric libraries: of 584 real tracks 38 still have no lyrics — 19 instrumental/folk/sfx, 6 very short or untagged clips, 13 covers that neither library has (mostly covers and internet singers). The NetEase source is off by default; enable it in Settings when needed (it may be rate-limited by IP).

## Verified

- Real library (584 dirty files from media sites, neither tags nor filenames clean): **498/584 (85.3%)** with lyrics, **48** instrumental placeholders all marked "no lyrics, instrumental", **498/511 (97.5%)** after excluding instrumental/sfx/very short clips; spot checks on `做你的爱人` (285s original), `相思` (红豆生南国…), `晚风作酒` all match the original recordings — no misassignments.
- Instrumental detection and migration: one startup run corrected 48 historical placeholder caches; `GET /api/lyric` returns the conclusion for instrumental tracks in 0.03s without any network call; the pending count dropped from 48 to 38 and no longer loops forever.
- Library: 2 audio + 1 video, 3 lyrics, `cueCount` correct.
- 晴天 (same-name subtitle) 8 synchronized lines; Blank Space (LRCLIB) 82 lines; video SRT bilingual (Chinese primary line + English translation).
- Range: `206 Partial Content` with a correct `Content-Range`.
- Lyric fragment `故事的小黄花` → local hit "晴天 @0.5s", NetEase returns 10 rows including the matched lyric line.
- Player page: lyrics highlight line by line while scrolling, video subtitles overlay the picture, header/footer always visible.
- Title identification (fake AI endpoint + real LRCLIB): `abc1234.mp3` became "夜空中最亮的星 - 逃跑计划" via AI, then validated by LRCLIB at 0.99, written back with 33 synchronized lyric lines; when AI only offered "测试歌曲/测试歌手" (0.55), single-track identification returned `apply:false` and batch mode only suggested without persisting; with no AI configured the feature degrades to local candidates and does not error.
- Settings page: fill base/model/key → save → "Test connection" echoes the model name, and `/api/config` returns only the `aiConfigured` boolean.

**Security (v1.4.1)**: 9 new unit cases in `internal/netguard` (IP ranges, request origin, same-origin) and `internal/server` (proxy 403, token, same-origin, redaction); black-box re-tests with `X-Forwarded-For` on a tokenless instance returned 403 for `/api/config`, `/api/library`, `/api/stats`, `/media/*`, `/cover/*` while local direct access stayed 200; a token + proxy browser session loaded 584 tracks and played normally with `errors: []`; the previous path-traversal payloads still return 404 with no content.
