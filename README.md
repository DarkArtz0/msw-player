# mswplayr — manual-network fork

A native Windows video player built with Go, MyGo, and mpv. The interface takes its typography and quiet light/dark palette from MiniMuse.

The app icon is **Cadence / forward**, selected from twenty distinct concepts through two critiques by Claude Opus 5.5. The [design record](docs/icon-design/README.md) includes the drafts, shortlist, critique, and final assets. The SVG master, 1024px PNG, native-size PNGs, and Windows ICO are in `resources/`.

Add video folders to build your library, search or filter by source folder, then play a video. Settings manages saved sources and appearance. Subfolders are scanned automatically; unavailable sources are reported without removing them. Refresh rescans for new files. You can also drop folders or individual videos from Explorer, or pass paths on the command line.

The player keeps only the timeline and transport controls visible. The picture-in-picture button moves the video into a small window that stays above other windows while you browse the library. Drag it by its control strip, resize it from its edges, and use its buttons to play or pause, return to the player, or close it (which pauses). Double-clicking the video also returns to the player, and Escape closes it. Settings opens in its own owned window, leaving the current library/player view and playback intact. The queue and chapter list open as modals, captions and audio open as exact track selection menus, and codec/resolution details live in **Stats for nerds**. Fullscreen shows only the video; double-click the video or use F/Escape to return.

Click a library video once to select it; click it again to open it. Its thumbnail expands into the player. Watched videos resume at their saved position, while unwatched videos start at the beginning after the thumbnail fades. The transition respects the system's reduced-motion preference.

Thumbnails use a preview from inside unwatched videos, skipping dark frames when possible, and the exact saved playback position for watched videos. A single background mpv worker extracts frames for visible rows, with a bounded in-memory cache and one disk thumbnail per source file. Failed loads are retried, and damaged cached images are regenerated. Unreadable or unsupported files retain a simple video placeholder. Returning to the library saves the current position. A paused video stays paused; a playing video continues in picture-in-picture.

## Run

With Go 1.27.3 and 7-Zip installed:

```powershell
./scripts/setup-mpv.ps1
go run .
# Or build a portable folder:
./scripts/build.ps1
./build/mswplayr.exe
```

Keep `mpv.exe` and `d3dcompiler_43.dll` beside the packaged player. During development, the player also finds `runtime/mpv.exe`, an executable on PATH, or the path in `MSWPLAYR_MPV`.

## Controls

| Key | Action |
| --- | --- |
| Ctrl+O | Add video folder |
| Space | Play / pause |
| Left / Right | Seek five seconds |
| M | Mute |
| F | Fullscreen |
| Escape | Leave fullscreen |
| Page Up / Page Down | Previous / next chapter |
| Double-click video | Toggle fullscreen |

The audio menu lists each available audio track by title, language, and ID, highlights the current track, and offers Off. The caption menu lists each subtitle track by title, language, and ID, including external/forced/SDH indicators where available. Choose **Off** to disable captions or **Load subtitle file** for SRT, ASS, SSA, VTT, SUB, and IDX files. mpv also discovers matching external subtitle files. The speed button cycles 1, 1.25, 1.5, 2, and 0.75 times normal speed.

The chapter button lists embedded chapter names and timestamps, highlights the current chapter, and provides previous/next controls. Selecting a chapter seeks to it while preserving pause state. The button is disabled for videos without chapter metadata.

**Subtitle appearance** is available from the caption menu or Settings. Font, size, text color, outline, shadow, vertical position, bold text, and a dark background are applied live and saved. The Reset button restores defaults. Text styling does not change image-based captions; styled ASS/SSA captions keep their authored styling unless **Apply to styled ASS/SSA captions** is enabled.

Online subtitle lookup is manual only in this fork. Playback and startup never trigger subtitle searches or downloads. Existing embedded or sidecar subtitles remain available. The player volume control and mpv maximum both extend to 300%.

There is no app auto-updater or startup version check. Updates require an explicit manual installation. The application starts at its saved local-folder thumbnail library before playback; no online metadata account is needed.

**Find subtitles online** opens an in-app results dialog. The title is derived from the filename, including year or season/episode. **Find and load best** downloads and selects the strongest release match; each result can also be downloaded individually. Ambiguous titles are shown for selection instead of guessing a different film. Weak release matches remain available for manual selection. There is no browser download step.

The default provider is the public [OpenSubtitles v3 add-on](https://opensubtitles-v3.strem.io/manifest.json), with [Cinemeta](https://v3-cinemeta.strem.io/manifest.json) for title identification, through the documented [Stremio subtitle protocol](https://github.com/Stremio/stremio-addon-sdk/blob/master/docs/api/requests/defineSubtitlesHandler.md). No API key is required for this public service at present. Availability and rate limits depend on those services. Errors and empty results are reported in the app; no repeated automatic retry loop runs.

Matching sends the search title, the video's basename, file size, and an OpenSubtitles file hash when available; video contents and full local paths are not uploaded. Cached downloads live in `%APPDATA%/mswplayr/subtitles` (or under `MSWPLAYR_DATA`), so reopening the same unchanged file and language can work offline. Downloads have size limits, support raw/ZIP/gzip subtitle data, are checked for actual subtitle cues, and are written using app-generated filenames. Changing videos, changing language, closing the dialog, or cancelling a request prevents stale results from loading into the player.

## Playback and performance

MKV, MOV, and MP4 are containers. Their actual video and audio codecs determine decoding support. mpv's FFmpeg backend handles common H.264, HEVC, VP9, AV1, AAC, and Opus media, among others. Encrypted/DRM media is outside this player's scope.

Video renders directly into an embedded native window through mpv's GPU renderer, without copying decoded frames through Go or the UI. Hardware decoding is requested with software fallback. Demux buffers are bounded to 64 MiB forward and 16 MiB backward. The UI observes state at most four times per second and redraws only when state changes; the queue builds visible rows only. These are implementation choices, not a measured guarantee of 4K performance on every GPU.

This version targets Windows x64. Folder sources, appearance, density, and per-video playback history are saved to `%APPDATA%/mswplayr/settings.json`; set `MSWPLAYR_DATA` to override that directory. Settings offers Compact, Comfortable, and Spacious layouts. Scrollable lists reserve a 16-DIP gutter beside their contents. The dark theme uses neutral black and gray surfaces and controls.

Positions are checkpointed every ten seconds during playback and on pause, navigation, video changes, and normal exit. Finished videos restart from the beginning. Thumbnails are stored in the `thumbnails` subdirectory of the settings directory. Scans and settings writes run in background workers. Removing a source changes the library only. The playback queue lasts for the session.

Windows always draws mpv's native video window above the MyGo UI. Dialogs therefore hide the video while open. The caption and audio menus keep it playing: the video window's region is cut away under the open menu, so the menu shows through. That gap shows the dark viewport, not the video, so the menu's glass frosts black.

### Future work

- **Frosted track menus over live video.** Show the windowed caption and audio menus in an owned acrylic window, as fullscreen already does (`layoutFullscreenPicker`), so the system blurs the playing video behind them. This replaces the region cut in `layoutHost`. It needs the window placed against its trigger in screen coordinates, kept in place as the main window moves or resizes, and dismissed when the main window loses focus.

## Verification

```powershell
$env:CGO_ENABLED='0'
go test ./...
go tool mygo vet .
go vet ./...
# Requires ffmpeg and the mpv runtime:
$env:MSWPLAYR_INTEGRATION='1'
go test -run Integration -v
# Optional live subtitle search/download (also loads it into mpv if integration is enabled):
$env:MSWPLAYR_SUBTITLE_LIVE='1'
go test -run TestSubtitleProviderLive -v
# Optional UI renders in build/:
$env:MSWPLAYR_RENDER='1'
go test -run TestRender
```

Integration tests generate H.264 MP4/MKV/MOV, HEVC MKV, VP9 WebM, and AV1 MP4 samples with audio and verify loading, timeline progression, pause, seek, and resume. Separate multitrack MKV tests verify exact subtitle/audio selection, chapter seeks, the native double-click/Escape bindings, and external subtitle loading. A captured video frame verifies that subtitle color styling reaches the renderer. UI and filesystem tests cover fullscreen, separate settings presentation, modal transitions, saved styling, caption selection, source persistence, filtering, duplicate sources, and cancelled scans. These tests do not measure frame drops, subjective audio synchronization, or HDR correctness.

## Dependencies

The setup script pins the [20261102 Windows mpv build](https://github.com/shinchiro/mpv-winbuild-cmake/releases/tag/20261102) and checks the release asset SHA-256 digest when supplied by GitHub. mpv is a separate process controlled through its local named-pipe JSON IPC. See the [mpv manual](https://mpv.io/manual/stable/) for rendering, decoding, and IPC options.

Martian Grotesk is distributed under the bundled [SIL Open Font License](resources/fonts/OFL.txt). MyGo and go-winio retain their upstream licenses. The downloaded mpv/FFmpeg build has its own GPL and third-party licensing obligations; consult its [build repository](https://github.com/shinchiro/mpv-winbuild-cmake), [mpv sources](https://github.com/mpv-player/mpv), and [FFmpeg sources](https://ffmpeg.org/) before redistribution. The generated portable folder is a local development build, not a completed redistribution package.
