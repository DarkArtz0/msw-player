package main

import (
	"fmt"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"path/filepath"
	"strconv"
	"time"
)

func (a *app) view(c *ui.Context) {
	c.SetTheme(a.theme())
	t := c.Theme()
	c.Root().Background(t.Background)
	c.OnShortcut(ui.Ctrl, ui.KeyO, a.browseFolder)
	if a.page == "player" {
		c.OnShortcut(0, ui.KeySpace, a.toggle)
		c.OnShortcut(0, ui.KeyLeft, func() { a.jump(-5) })
		c.OnShortcut(0, ui.KeyRight, func() { a.jump(5) })
		c.OnShortcut(0, ui.KeyF, a.toggleFullscreen)
		c.OnShortcut(0, ui.KeyM, func() { a.send("cycle", "mute") })
		c.OnShortcut(0, ui.KeyPageUp, func() { a.stepChapter(-1) })
		c.OnShortcut(0, ui.KeyPageDown, func() { a.stepChapter(1) })
	}
	c.OnShortcut(0, ui.KeyEscape, func() {
		if a.fullscreen {
			a.exitFullscreen()
		}
	})
	if a.fullscreen {
		viewport := ui.Box(c).Label("Video player").Fill().Background(ui.Hex("#000000"))
		if viewport.DoubleClicked() {
			a.toggleFullscreen()
		}
		return
	}
	ui.Column(c).Fill().Children(func() {
		a.titlebar(c)
		switch a.page {
		case "player":
			a.playerView(c)
		default:
			a.libraryView(c)
		}
	})
	a.queueModal(c)
	a.onlineModal(c)
	a.statsModal(c)
	a.chapterModal(c)
	a.settingsFallback(c)
	a.transitionView(c)
}
func (a *app) titlebar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().Height(44).Shrink(0).Padding(0, 12, 0, 20).AlignItems(ui.Center).Gap(9).DragWindow().Children(func() {
		ui.Icon(c, brandMark).Size(19, 19).TextColor(t.Text)
		ui.Text(c, "mswplayr").Bold()
		ui.Spacer(c)
		icon(c, "settings", "Settings").OnClick(a.openSettings)
		icon(c, "min", "Minimize").OnClick(func() {
			if a.window != nil {
				a.window.Minimize()
			}
		})
		icon(c, "max", "Maximize").OnClick(func() {
			if a.window != nil {
				if a.window.IsMaximized() {
					a.window.Unmaximize()
				} else {
					a.window.Maximize()
				}
			}
		})
		icon(c, "close", "Close").OnClick(func() {
			if a.window != nil {
				a.window.Close()
			}
		})
	})
}
func (a *app) libraryView(c *ui.Context) {
	t := c.Theme()
	d := a.densityMetrics()
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).Padding(16, 20, 20, 20).Gap(d.gap).Children(func() {
		ui.Column(c).Width(180).Shrink(0).FillHeight().Gap(8).Children(func() {
			ui.Text(c, "Library").FontSize(16).Bold().Padding(8, 10)
			choice(c, "library-nav", "All videos", a.folderFilter == "").FillWidth().OnClick(func() { a.folderFilter = ""; a.filterLibrary() })
			ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Padding(0, scrollbarGutter, 0, 0).Children(func() {
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					for _, path := range a.folders {
						choice(c.Key(path), "library-nav", filepath.Base(path), a.folderFilter == path).FillWidth().Tooltip(path).OnClick(func() { a.folderFilter = path; a.filterLibrary() })
					}
				})
			})
			if a.current >= 0 {
				ui.Button(c, "Now playing").OnClick(func() { a.navigate("player") })
			}
			ui.Button(c, "Add folder").OnClick(a.browseFolder)
		})
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Background(t.Surface).Radius(12).Padding(d.panel).Gap(d.gap).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
				heading := "Videos"
				if a.folderFilter != "" {
					heading = filepath.Base(a.folderFilter)
				}
				ui.Text(c, heading).FontSize(22).Bold().SingleLine().MinWidth(0).Grow(1)
				ui.TextInput(c.Key("library-search"), &a.search).Width(220).Placeholder("Search videos").Label("Search videos").OnChange(a.filterLibrary)
				icon(c, "refresh", "Refresh library").Disabled(a.scanning).OnClick(a.rescan)
				icon(c, "queue", "Play queue").OnClick(func() { a.showQueue = true; a.layoutHost() })
			})
			if a.libraryNotice != "" {
				ui.Text(c, a.libraryNotice).TextColor(t.TextMuted)
			}
			ui.Checkbox(c.Key("group-by-folder"), &a.groupByFolder, "Group by folder").OnChange(func() { a.filterLibrary(); a.persist() })
			if a.scanning {
				ui.Row(c.Key("scanning")).AlignItems(ui.Center).Gap(8).Transition(ui.ElementTransition{Duration: 180 * time.Millisecond, Ease: easeOut, Enter: &ui.Motion{Y: -4}, Exit: &ui.Motion{}}).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, "Scanning folders…").TextColor(t.TextMuted)
				})
			}
			if len(a.filtered) == 0 {
				ui.Column(c).FillWidth().Grow(1).Center().Gap(16).Children(func() {
					ui.Icon(c, icons["open"]).Size(40, 40).TextColor(t.TextMuted)
					heading, msg := "Add your video folders", "Your videos will appear here."
					if len(a.folders) > 0 {
						heading, msg = "No videos found", "Try another search or add a folder."
					}
					ui.Text(c, heading).FontSize(22).Bold()
					ui.Text(c, msg).TextColor(t.TextMuted)
					ui.Button(c, "Choose folder").OnClick(a.browseFolder)
				})
			} else {
				videos := a.filtered
				rows := a.libraryRows
				ui.Box(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
					ui.List(c.Key("library-videos"), &a.libraryList, len(rows)).Fill().Padding(0, scrollbarGutter, 0, 0).TrackScroll(&a.libraryScroll).ItemKey(func(i int) any {
						if rows[i].videoIndex < 0 {
							return struct{ Folder string }{rows[i].folder}
						}
						return videos[rows[i].videoIndex].Path
					}).Rows(func(row ui.ListRow) {
						c := row.Context
						item := rows[row.Index]
						if item.videoIndex < 0 {
							ui.Row(c).FillWidth().Height(40).AlignItems(ui.Center).Gap(12).Padding(6).Children(func() {
								ui.Text(c, filepath.Base(item.folder)).Bold().SingleLine().MinWidth(0).Tooltip(item.folder)
								count := fmt.Sprintf("%d videos", item.count)
								if item.count == 1 {
									count = "1 video"
								}
								ui.Text(c, count).FontSize(12).TextColor(t.TextMuted)
								ui.Text(c, item.folder).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0).Grow(1)
							})
							return
						}
						i := item.videoIndex
						v := videos[i]
						entry := a.thumbnailFor(v.Path)
						if entry.image == nil && !entry.pending && a.thumbJobs != nil {
							c.After(max(50*time.Millisecond, time.Until(entry.retryAt)))
						}
						ui.Row(c).FillWidth().Height(d.row).Padding(4, 0).Gap(8).AlignItems(ui.Center).Children(func() {
							button := ui.ButtonBase(c.Key("video")).Label("Video: " + v.Name).Grow(1).MinWidth(0).FillHeight().Padding(6).Gap(12).AlignItems(ui.Center).Radius(10).Background(ui.Transparent).Transition(colorFade)
							if a.selectedVideo == v.Path {
								button.Background(t.Selection)
							} else if button.Hovered() {
								button.Background(t.SurfaceHover)
							}
							press(button, .985)
							button.Children(func() {
								ui.Box(c.Key("thumbnail")).Bind(&entry.handle).Size(d.thumbnailW, d.thumbnailH).Shrink(0).Radius(6).Clip().Background(ui.Hex("#080808")).Center().Children(func() {
									if entry.image != nil {
										// The frame leans in a touch under the pointer.
										zoom := ui.Pose{}
										if button.Hovered() {
											zoom.Scale = 1.04
										}
										ui.Image(c.Key("image"), entry.image).Fill().Fit(ui.Contain).Transition(thumbnailFade).AnimatePose(zoom, ui.Animation{Duration: 240 * time.Millisecond, Ease: easeOut})
									} else {
										ui.Icon(c, icons["video"]).Size(24, 24).TextColor(t.TextMuted)
									}
								})
								ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
									ui.Text(c, v.Name).Bold().SingleLine()
									rel, _ := filepath.Rel(v.Folder, filepath.Dir(v.Path))
									folder := filepath.Base(v.Folder)
									if rel != "." && rel != "" {
										folder = filepath.Join(folder, rel)
									}
									ui.Text(c, folder).FontSize(12).TextColor(t.TextMuted).SingleLine()
								})
								if pos := a.resumePosition(v.Path); pos > 0 {
									ui.Text(c, clock(pos)).FontSize(12).FontFeatures("tnum").TextColor(t.TextMuted).Shrink(0)
								}
							}).OnClick(func() { a.selectVideo(c, i, entry) })
							// The queue button turns into a check for a moment to confirm.
							name := "queue"
							if left := queuedFeedback - c.Now().Sub(a.queuedAt); a.queuedPath == v.Path && left > 0 {
								name = "check"
								c.After(left)
							}
							added := icon(c, name, "Add to queue: "+v.Name).OnClick(func() {
								a.queue = append(a.queue, v.Path)
								a.queuedPath, a.queuedAt = v.Path, c.Now()
							})
							if name == "check" {
								added.TextColor(t.Accent)
							}
						})
					})
					// Rows scrolled under the panel's top edge frost as they go.
					if edge := min(a.libraryScroll.Y/24, 1); edge > 0 {
						ui.Box(c.Key("scroll-edge")).Absolute().Left(0).Top(0).Right(scrollbarGutter).Height(20).PassThrough().
							Material(glass.Blur{Radius: 4 * edge, Mask: &ui.LinearGradient{From: ui.RGB(0, 0, 0), To: ui.Transparent, Angle: 180}})
					}
				})
			}
		})
	})
}

// queuedFeedback is how long an add-to-queue button shows its check.
const queuedFeedback = 1200 * time.Millisecond

func navButton(c *ui.Context, label string, active bool) ui.Element {
	t := c.Theme()
	b := ui.Button(c, label).FillWidth().Padding(12).Background(ui.Transparent).Border(0, ui.Transparent).Transition(colorFade)
	if active {
		b.Background(t.Selection).TextColor(t.Accent)
	}
	return b
}
func (a *app) settingsView(c *ui.Context) {
	t := c.Theme()
	d := a.densityMetrics()
	ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Padding(20, 32+scrollbarGutter, 32, 32).Children(func() {
		ui.Column(c).FillWidth().Gap(d.gap).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Settings").FontSize(22).Bold()
				ui.Spacer(c)
				ui.Button(c, "Done").OnClick(a.closeSettings)
			})
			if a.store != nil {
				if err := a.store.lastError(); err != nil {
					ui.Text(c, "Could not save settings: "+err.Error()).TextColor(t.TextMuted)
				}
			}
			ui.Column(c).FillWidth().Background(t.Surface).Radius(12).Padding(d.panel).Gap(d.gap).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Video folders").FontSize(16).Bold()
					ui.Spacer(c)
					ui.Button(c, "Add folder").OnClick(a.browseFolder)
				})
				ui.Text(c, "Videos in these folders and their subfolders appear in your library.").TextColor(t.TextMuted)
				if len(a.folders) == 0 {
					ui.Text(c, "No folders added.").TextColor(t.TextMuted)
				}
				for _, path := range append([]string(nil), a.folders...) {
					ui.Row(c.Key(path)).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
						ui.Icon(c, icons["open"]).Size(20, 20).TextColor(t.TextMuted)
						ui.Text(c, path).SingleLine().Grow(1).MinWidth(0)
						ui.Button(c, "Remove").Label("Remove " + path).OnClick(func() { a.removeFolder(path) })
					})
				}
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Button(c, "Rescan folders").Disabled(a.scanning).OnClick(a.rescan)
					if a.scanning {
						ui.Text(c, "Scanning…").TextColor(t.TextMuted)
					}
				})
				if a.libraryNotice != "" {
					ui.Text(c, a.libraryNotice).TextColor(t.TextMuted)
				}
			})
			ui.Column(c).FillWidth().Background(t.Surface).Radius(12).Padding(d.panel).Gap(d.gap).Children(func() {
				ui.Text(c, "Appearance").FontSize(16).Bold()
				ui.Checkbox(c, &a.dark, "Dark appearance").OnChange(a.persist)
				ui.Text(c, "Interface density").Bold()
				ui.Row(c).Gap(8).Children(func() {
					for _, density := range []string{"Compact", "Comfortable", "Spacious"} {
						choice(c, "density", density, a.density == density).OnClick(func() { a.density = density; a.persist() })
					}
				})
			})
			ui.Column(c).FillWidth().Background(t.Surface).Radius(12).Padding(d.panel).Gap(d.gap).Children(func() {
				ui.Text(c, "Subtitles").FontSize(16).Bold()
				ui.Select(c, &a.subtitleLanguage, subtitleLanguages).Label("Preferred subtitle language").Width(240).OnChange(a.subtitlePreferencesChanged)
				ui.Text(c, "Online subtitles are searched only when you request them.").TextColor(t.TextMuted)
			})
			a.subtitleAppearanceView(c)
		})
	})
}
func (a *app) playerView(c *ui.Context) {
	title := "Nothing playing"
	if a.current >= 0 {
		title = filepath.Base(a.queue[a.current])
	}
	ui.Row(c).FillWidth().Height(56).Shrink(0).Padding(0, 20).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Button(c, "Library").OnClick(func() { a.navigate("library") })
		ui.Text(c, title).FontSize(16).Bold().SingleLine().Grow(1).MinWidth(0)
		icon(c, "queue", "Play queue").OnClick(func() { a.showQueue = true; a.layoutHost() })
		icon(c, "more", "Stats for nerds").OnClick(func() { a.statsOpen = true; a.layoutHost() })
		icon(c, "chapters", "Chapters").Disabled(!a.status.Loaded || len(chapters(a.status.ChapterJSON)) == 0).OnClick(func() { a.chaptersOpen = true; a.captionsOpen = false; a.audioOpen = false; a.layoutHost() })
	})
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).Padding(0, 20).Children(func() {
		viewport := ui.Column(c).Label("Video player").Grow(1).FillHeight().MinWidth(0).Background(ui.Hex("#080808")).Radius(12).Center().Gap(12).Children(func() {
			if !a.status.Loaded || a.notice != "" {
				heading := "Opening video"
				if a.notice != "" {
					heading = "Playback unavailable"
				}
				if a.notice == "" {
					ui.Spinner(c).Size(20, 20).TextColor(ui.Hex("#a4afc4"))
				}
				ui.Text(c, heading).FontSize(16).Bold().TextColor(ui.Hex("#edf0f8"))
				if a.notice != "" {
					ui.Text(c, a.notice).MaxWidth(520).TextAlign(ui.Center).TextColor(ui.Hex("#a4afc4"))
				}
			}
		})
		if viewport.DoubleClicked() {
			a.toggleFullscreen()
		}
	})
	a.playbackControls(c)
}
func (a *app) playbackControls(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Height(104).FillWidth().Shrink(0).Padding(12, 24, 12, 24).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(14).Children(func() {
			ui.Text(c, clock(a.seek)).FontSize(12).FontFeatures("tnum").Width(58).TextColor(t.TextMuted)
			ui.Slider(c.Key("seek"), &a.seek, 0, max(1, a.status.Duration)).Grow(1).Label("Seek").Disabled(!a.status.Loaded).OnChange(func() {
				// Hold the thumb where the pointer is until mpv catches up.
				a.seekHeld = time.Now().Add(500 * time.Millisecond)
				a.send("seek", a.seek, "absolute")
			})
			ui.Text(c, clock(a.status.Duration)).FontSize(12).FontFeatures("tnum").Width(58).TextAlign(ui.End).TextColor(t.TextMuted)
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			icon(c, "volume", "Mute").OnClick(func() { a.send("cycle", "mute") })
			ui.Slider(c.Key("volume"), &a.volume, 0, 300).Width(88).Label("Volume").OnChange(func() { a.send("set_property", "volume", a.volume) })
			ui.Spacer(c)
			icon(c, "back", "Previous video").Disabled(a.current <= 0).OnClick(func() { a.play(a.current - 1) })
			a.chapterStepButton(c, -1)
			name, label := "play", "Play"
			if a.status.Loaded && !a.status.Paused && !a.status.Ended {
				name, label = "pause", "Pause"
			}
			keyedIcon(c, "play-pause", name, label).Size(40, 40).Background(t.Accent).TextColor(t.AccentText).Disabled(!a.status.Loaded).OnClick(a.toggle)
			a.chapterStepButton(c, 1)
			icon(c, "next", "Next video").Disabled(a.current+1 >= len(a.queue)).OnClick(func() { a.play(a.current + 1) })
			ui.Spacer(c)
			a.speedButton(c)
			audio := a.audioMenu(c)
			captions := a.captionMenu(c)
			a.cutMenuHole(audio, captions)
			icon(c, "pip", "Picture in picture").Disabled(!a.status.Loaded).OnClick(a.togglePip)
			icon(c, "expand", "Fullscreen").Disabled(!a.status.Loaded).OnClick(a.toggleFullscreen)
		})
	})
}

func (a *app) captionMenu(c *ui.Context) ui.Element {
	trigger := icon(c, "sub", "Captions").OnClick(func() { a.captionsOpen = !a.captionsOpen; a.audioOpen = false; a.layoutHost() })
	if a.fullscreen {
		return ui.Element{}
	}
	popup := frost(c, enterFrom(ui.Popover(c, trigger, &a.captionsOpen, func() { a.captionPicker(c) }), trigger))
	if popup.Dismissed() {
		a.layoutHost()
	}
	return popup
}
func (a *app) captionPicker(c *ui.Context) {
	ui.Column(c).Width(300).MaxHeight(360).Gap(4).Children(func() {
		ui.Text(c, "Captions").FontSize(16).Bold().Padding(8)
		if status := a.subtitleStatus(); status != "" {
			ui.Text(c, status).TextColor(c.Theme().TextMuted).Padding(4, 8)
		}
		ui.Scroll(c).FillWidth().MaxHeight(160).MinHeight(0).Padding(0, scrollbarGutter, 0, 0).Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				a.captionOption(c, "Off", "no")
				for _, track := range subtitleTracks(a.status.TrackJSON) {
					a.captionOption(c, track.label(), strconv.Itoa(track.ID))
				}
			})
		})
		ui.Button(c, "Load subtitle file…").FillWidth().Shrink(0).OnClick(a.loadSubtitle)
		ui.Button(c, "Find subtitles online…").FillWidth().Shrink(0).Disabled(a.current < 0).OnClick(a.openSubtitleSearch)
		ui.Button(c, "Subtitle appearance…").FillWidth().Shrink(0).OnClick(a.openSubtitleAppearance)
	})
}

func (a *app) captionOption(c *ui.Context, label, id string) {
	t := c.Theme()
	b := ui.Button(c.Key(id), label).FillWidth().Border(0, ui.Transparent).Background(ui.Transparent).Transition(colorFade)
	if a.subtitleID == id {
		b.Background(t.Selection).TextColor(t.Accent)
	}
	b.OnClick(func() { a.chooseSubtitle(id) })
}
func (a *app) queueModal(c *ui.Context) {
	w, h := c.Size()
	dialog := frostedModal(c, &a.showQueue, func() {
		ui.Column(c).Width(min(520, w-80)).Height(min(460, h-120)).Gap(16).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Play queue").FontSize(16).Bold()
				ui.Spacer(c)
				icon(c, "close", "Close queue").OnClick(func() { a.showQueue = false; a.layoutHost() })
			})
			if len(a.queue) == 0 {
				ui.Text(c, "No videos queued.").TextColor(c.Theme().TextMuted)
			}
			queue := a.queue
			ui.List(c.Key("queue-list"), &a.queueList, len(queue)).FillWidth().Grow(1).MinHeight(0).Padding(0, scrollbarGutter, 0, 0).ItemKey(func(i int) any { return i }).Rows(func(row ui.ListRow) {
				i := row.Index
				c := row.Context
				navButton(c, filepath.Base(queue[i]), i == a.current).OnClick(func() { a.play(i) })
			})
			ui.Row(c).FillWidth().Gap(12).Children(func() {
				ui.Button(c, "Choose from library").OnClick(func() { a.navigate("library") })
				ui.Spacer(c)
				ui.Button(c, "Clear upcoming").Disabled(a.current < 0 || a.current+1 >= len(a.queue)).OnClick(func() { a.queue = a.queue[:a.current+1] })
			})
		})
	})
	enter(dialog)
	if dialog.Dismissed() {
		a.layoutHost()
	}
}
func (a *app) onlineModal(c *ui.Context) {
	w, h := c.Size()
	dialog := frostedModal(c, &a.onlineOpen, func() {
		ui.Column(c).Width(min(560, w-80)).Height(min(560, h-100)).Gap(14).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Find subtitles").FontSize(16).Bold()
				ui.Spacer(c)
				icon(c, "close", "Close subtitle search").OnClick(a.closeSubtitleSearch)
			})
			ui.TextInput(c, &a.subtitleQuery).FillWidth().Label("Video title").Placeholder("Movie or episode title").OnChange(a.subtitleQueryEdited)
			ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Select(c, &a.subtitleLanguage, subtitleLanguages).Label("Subtitle language").Grow(1).OnChange(a.subtitlePreferencesChanged)
				ui.Button(c, "Search").Disabled(a.online.Busy).OnClick(func() { a.findSubtitles(false, nil) })
				ui.PrimaryButton(c, "Find and load best").Disabled(a.online.Busy).OnClick(func() {
					if len(a.online.Matches) > 0 {
						a.downloadSubtitle(a.online.Matches[0])
					} else {
						a.findSubtitles(true, nil)
					}
				})
			})
			if status := a.subtitleStatus(); status != "" {
				ui.Text(c, status).TextColor(c.Theme().TextMuted)
			}
			if a.online.Busy {
				ui.Button(c, "Cancel request").OnClick(func() { a.cancelSubtitleJob(); a.online.Message = "Request cancelled." })
			}
			ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Padding(0, scrollbarGutter, 0, 0).Children(func() {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					if a.online.Title.ID == "" {
						for _, title := range a.online.Titles {
							ui.Button(c, title.Name+" ("+title.Year+")").FillWidth().Disabled(a.online.Busy).OnClick(func() { a.findSubtitles(true, &title) })
						}
					}
					for _, sub := range a.online.Matches {
						ui.Row(c.Key(sub.ID + sub.URL)).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
							ui.Text(c, sub.name()).SingleLine().Grow(1).MinWidth(0).Tooltip(sub.name())
							ui.Button(c, "Download and load").Label("Download " + sub.ID).Disabled(a.online.Busy).OnClick(func() { a.downloadSubtitle(sub) })
						})
					}
				})
			})
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "OpenSubtitles").FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Spacer(c)
				ui.Button(c, "Load local file…").OnClick(a.loadSubtitle)
			})
		})
	})
	enter(dialog)
	if dialog.Dismissed() {
		a.closeSubtitleSearch()
	}
}
func (a *app) statsModal(c *ui.Context) {
	dialog := frostedModal(c, &a.statsOpen, func() {
		ui.Column(c).Width(360).Gap(16).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Stats for nerds").FontSize(16).Bold()
				ui.Spacer(c)
				icon(c, "close", "Close stats").OnClick(func() { a.statsOpen = false; a.layoutHost() })
			})
			ui.Text(c, fmt.Sprintf("Video: %d × %d", a.status.Width, a.status.Height))
			ui.Text(c, "Codec: "+a.status.Codec)
			ui.Text(c, "Decoder: "+a.status.Decoder)
		})
	})
	enter(dialog)
	if dialog.Dismissed() {
		a.layoutHost()
	}
}

var speeds = []float64{1, 1.25, 1.5, 2, 0.75}

func (a *app) speed() float64 { return speeds[a.speedIndex] }
func (a *app) cycleSpeed() {
	before := a.speed()
	a.speedIndex = (a.speedIndex + 1) % len(speeds)
	a.speedDown = a.speed() < before
	a.send("set_property", "speed", a.speed())
}

// speedButton rolls its digits to the next speed, down when it wraps to slower.
func (a *app) speedButton(c *ui.Context) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("speed")).Label("Playback speed").Width(56).Height(36).Center().Radius(8).Background(ui.Transparent).TextColor(t.Text).Transition(colorFade)
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	press(b, .94)
	b.Children(func() {
		ui.Text(c, fmt.Sprintf("%gx", a.speed())).FontFeatures("tnum").SingleLine().ContentTransition(ui.ContentTransition{Numeric: true, Down: a.speedDown})
	})
	b.OnClick(a.cycleSpeed)
}
