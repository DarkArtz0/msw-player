package main

import (
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"
)

type app struct {
	window                                          *mygo.Window
	engine                                          *player
	status                                          playback
	queue                                           []string
	current                                         int
	speedIndex                                      int
	showQueue, dark                                 bool
	volume, seek                                    float64
	notice                                          string
	host, hostParent                                uintptr
	done                                            chan struct{}
	pollEngine                                      atomic.Pointer[player]
	hostGeometry                                    [4]int
	hostVisible                                     bool
	hostBehind                                      bool
	hostHole                                        [4]int
	menuHole                                        ui.Rect
	controlsWindow                                  *mygo.Window
	controlsVisible                                 bool
	controlsUntil                                   time.Time
	controlsCursor                                  nativePoint
	controlsCursorKnown                             bool
	queueList                                       ui.ListState
	page                                            string
	fullscreen, captionsOpen, onlineOpen, statsOpen bool
	folders                                         []string
	library, filtered                               []videoFile
	folderFilter, search                            string
	libraryList                                     ui.ListState
	libraryScroll                                   ui.ScrollState
	queuedPath                                      string
	queuedAt                                        time.Time
	speedDown                                       bool
	groupByFolder                                   bool
	libraryRows                                     []libraryRow
	pickerWindow                                    *mygo.Window
	pickerVisible                                   bool
	pickerKind                                      string
	pickerContent                                   ui.Handle
	pickerSize                                      ui.Rect
	pickerSerial, pickerMeasured, pickerReady       uint64
	pickerOpened                                    time.Time
	pendingSubtitle, pendingAudio, pendingChapter   optimistic
	seekHeld                                        time.Time
	scanning                                        bool
	scanGeneration                                  uint64
	scanCancel                                      func()
	libraryNotice                                   string
	store                                           *settingsStore
	subtitleID                                      string
	subtitleQuery, subtitleLanguage                 string
	density                                         string
	selectedVideo                                   string
	history                                         map[string]bookmark
	lastCheckpoint                                  time.Time
	thumbs                                          map[string]*thumbnail
	thumbJobs                                       chan thumbnailJob
	thumbCancel                                     func()
	thumbClock                                      uint64
	cacheDir                                        string
	transition                                      *openTransition
	minPlaybackGeneration                           uint64
	online                                          subtitleDownloadState
	subtitleService                                 *subtitleService
	subtitleRequest                                 uint64
	subtitlesDir                                    string
	settingsWindow                                  *mygo.Window
	settingsOpen                                    bool
	settingsTarget                                  string
	audioOpen, chaptersOpen                         bool
	chapterList                                     ui.ListState
	subtitleStyle                                   subtitleAppearance
	appliedSubtitleStyle                            subtitleAppearance
	styleApplied                                    bool
	pip, pipVisible                                 bool
	pipWindow                                       *mygo.Window
}

func newApp() *app {
	return &app{current: -1, volume: 80, page: "library", subtitleID: "no", subtitleLanguage: "English", subtitleStyle: defaultSubtitleAppearance(), density: "Comfortable", history: map[string]bookmark{}, thumbs: map[string]*thumbnail{}, done: make(chan struct{})}
}

// optimistic shows a choice the user just made until mpv reports it, so a
// snapshot taken before mpv applied it does not flip the menu back to the
// old value for a tick. A choice mpv never confirms gives way after a while.
type optimistic struct {
	value string
	until time.Time
}

const optimisticHold = 1500 * time.Millisecond

func (o *optimistic) set(value string, now time.Time) {
	o.value, o.until = value, now.Add(optimisticHold)
}

// resolve returns what to show given what mpv reported.
func (o *optimistic) resolve(reported string, now time.Time) string {
	if o.value == "" {
		return reported
	}
	if reported == o.value || now.After(o.until) {
		*o = optimistic{}
		return reported
	}
	return o.value
}

// applyStatus takes a playback snapshot, keeping the choices still on their
// way to mpv and the seek bar the user is dragging.
func (a *app) applyStatus(s playback) {
	now := time.Now()
	a.status = s
	a.status.AudioID = a.pendingAudio.resolve(s.AudioID, now)
	if chapter, err := strconv.Atoi(a.pendingChapter.resolve(strconv.Itoa(s.Chapter), now)); err == nil {
		a.status.Chapter = chapter
	}
	sid := s.SubtitleID
	if sid == "" {
		sid = "no"
	}
	a.subtitleID = a.pendingSubtitle.resolve(sid, now)
	if now.After(a.seekHeld) {
		a.seek = s.Position
	}
	if a.controlsWindow != nil {
		a.controlsWindow.Invalidate()
	}
	if a.pickerWindow != nil {
		a.pickerWindow.Invalidate()
	}
	if a.pipWindow != nil {
		a.pipWindow.Invalidate()
	}
}

func (a *app) send(args ...any) {
	if a.engine != nil {
		a.engine.command(args...)
	}
}
func (a *app) toggle() {
	if a.transition != nil {
		return
	}
	if a.status.Ended {
		a.send("seek", 0, "absolute")
		a.send("set_property", "pause", false)
	} else {
		a.send("cycle", "pause")
	}
}
func (a *app) jump(seconds float64) {
	if a.transition != nil {
		return
	}
	a.send("seek", seconds, "relative")
}
func (a *app) play(index int) {
	if index < 0 || index >= len(a.queue) {
		return
	}
	a.checkpoint(true)
	a.cancelSubtitleJob()
	a.online = subtitleDownloadState{}
	a.current = index
	a.pip = false
	a.subtitleQuery = videoSearchTitle(a.queue[index])
	a.page = "player"
	a.showQueue = false
	a.captionsOpen = false
	a.audioOpen = false
	a.chaptersOpen = false
	a.notice = ""
	if a.engine == nil || a.engine.snapshot().Error != "" {
		if a.engine != nil {
			a.engine.close()
		}
		a.engine = newPlayer(a.host)
		a.pollEngine.Store(a.engine)
	}
	a.status = playback{}
	a.subtitleID = "no"
	// Choices made for the last video must not hold onto this one.
	a.pendingSubtitle, a.pendingAudio, a.pendingChapter = optimistic{}, optimistic{}, optimistic{}
	a.seekHeld = time.Time{}
	a.engine.prepareLoad()
	a.minPlaybackGeneration = a.engine.snapshot().Generation
	start := a.resumePosition(a.queue[index])
	a.seek = start
	a.engine.command("loadfile", a.queue[index], "replace", -1, map[string]string{"start": fmt.Sprintf("%.3f", start), "pause": boolOption(a.transition != nil)})
	if a.transition == nil {
		a.engine.command("set_property", "pause", false)
	}
	a.engine.command("set_property", "volume", a.volume)
	a.engine.command("set_property", "speed", a.speed())
	a.styleApplied = false
	a.applySubtitleAppearance()
	a.layoutHost()
}

// advance plays the next queued video where the last one played, so a video
// in picture-in-picture is followed by the next one there.
func (a *app) advance() {
	pip := a.pip && a.page != "player"
	a.play(a.current + 1)
	if pip {
		a.pip, a.page = true, "library"
		a.layoutHost()
	}
}
func (a *app) add(paths []string) {
	first := len(a.queue)
	for _, path := range paths {
		if supported(path) {
			if abs, err := filepath.Abs(path); err == nil {
				a.queue = append(a.queue, abs)
			}
		}
	}
	if len(a.queue) > first {
		a.play(first)
	} else if len(paths) > 0 {
		a.notice = "Choose a video file such as MKV, MP4, MOV, or WebM."
	}
}
func (a *app) browse() {
	if a.window == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.window, Title: "Open videos", Multiple: true, Filters: []mygo.FileFilter{{Name: "Video", Extensions: videoExtensions}}})
		a.window.Update(func() {
			if err != nil {
				a.notice = err.Error()
			} else {
				a.add(paths)
			}
		})
	}()
}
func main() {
	a := newApp()
	base, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	if custom := os.Getenv("MSWPLAYR_DATA"); custom != "" {
		base = custom
	} else {
		base = filepath.Join(base, "mswplayr")
	}
	prefs, err := loadSettings(filepath.Join(base, "settings.json"))
	if err != nil {
		a.libraryNotice = "Settings could not be read: " + err.Error()
	}
	a.folders, a.dark = prefs.Folders, prefs.Dark
	a.groupByFolder = prefs.GroupByFolder
	if prefs.Density == "Compact" || prefs.Density == "Spacious" {
		a.density = prefs.Density
	}
	if prefs.History != nil {
		a.history = prefs.History
	}
	a.cacheDir = filepath.Join(base, "thumbnails")
	a.subtitlesDir = filepath.Join(base, "subtitles")
	if _, ok := languageCodes[prefs.SubtitleLanguage]; ok {
		a.subtitleLanguage = prefs.SubtitleLanguage
	}
	a.store = newSettingsStore(filepath.Join(base, "settings.json"))
	if prefs.SubtitleStyle != nil {
		a.subtitleStyle = normalizedSubtitleAppearance(*prefs.SubtitleStyle)
	}
	mygo.App.WhenReady(func() {
		a.window = mygo.NewWindow(mygo.WindowOptions{Title: "mswplayr", Width: 1120, Height: 760, MinWidth: 760, MinHeight: 540, StateKey: "main", Frameless: true, BackgroundColor: "#f1f3f8", Content: ui.View(a.view)})
		a.host = makeHost(a.window.NativeHandle())
		a.hostParent = a.window.NativeHandle()
		a.startThumbnailWorker()
		a.window.OnResize(a.layoutHost)
		// The track menus are popovers in the window and separate windows in
		// fullscreen: one open across the change would show in the wrong place.
		a.window.OnEnterFullScreen(func() {
			a.fullscreen, a.captionsOpen, a.audioOpen = true, false, false
			a.window.Invalidate()
			a.layoutHost()
		})
		a.window.OnLeaveFullScreen(func() {
			a.fullscreen = false
			a.dismissFullscreenPicker()
			a.hideFullscreenControls()
			a.window.Invalidate()
			a.layoutHost()
		})
		a.window.OnFileDrop(func(e *mygo.FileDropEvent) { a.importPaths(e.Paths) })
		a.window.OnClosed(func() {
			a.closeFullscreenControls()
			a.closeSettings()
			if a.pipWindow != nil {
				a.pipWindow.Close()
			}
			a.cancelSubtitleJob()
			a.checkpoint(true)
			close(a.done)
			if a.thumbCancel != nil {
				a.thumbCancel()
			}
			if a.engine != nil {
				a.engine.close()
			}
			if a.scanCancel != nil {
				a.scanCancel()
			}
			if a.store != nil {
				a.store.close()
			}
		})
		go func() {
			ticker := time.NewTicker(80 * time.Millisecond)
			defer ticker.Stop()
			var last playback
			var previous *player
			for {
				select {
				case <-a.done:
					return
				case <-ticker.C:
					a.window.Update(a.tickFullscreenControls)
					engine := a.pollEngine.Load()
					if engine == nil {
						continue
					}
					s := engine.snapshot()
					if engine == previous && s == last {
						continue
					}
					previous, last = engine, s
					a.window.Update(func() {
						if a.engine != engine || s.Generation < a.minPlaybackGeneration {
							return
						}
						a.applyStatus(s)
						a.drainPlayerInput(engine)
						a.subtitleTick()
						a.checkpoint(false)
						if s.Error != "" {
							a.notice = s.Error
						}
						if s.Ended && a.current+1 < len(a.queue) {
							a.advance()
						}
						a.layoutHost()
					})
				}
			}
		}()
		if len(os.Args) > 1 {
			a.importPaths(os.Args[1:])
		}
		a.rescan()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
