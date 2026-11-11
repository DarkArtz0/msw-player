package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/egoist/mygo/ui"
	"path/filepath"
	"strings"
	"time"
)

type subtitleDownloadState struct {
	Busy, AutoChecked            bool
	Message, Error, PendingTitle string
	Titles                       []subtitleTitle
	Matches                      []onlineSubtitle
	Title                        subtitleTitle
	List                         ui.ListState
	cancel                       context.CancelFunc
}

func (a *app) cancelSubtitleJob() {
	a.subtitleRequest++
	if a.online.cancel != nil {
		a.online.cancel()
		a.online.cancel = nil
	}
	a.online.Busy = false
}
func (a *app) subtitleCurrent(request uint64, path string) bool {
	return a.subtitleRequest == request && a.current >= 0 && a.current < len(a.queue) && a.queue[a.current] == path
}
func (a *app) subtitleTick() {
	if a.online.PendingTitle != "" {
		for _, track := range subtitleTracks(a.status.TrackJSON) {
			if track.External && track.Title == a.online.PendingTitle {
				a.online.Message = a.subtitleLanguage + " captions loaded."
				a.online.PendingTitle = ""
				break
			}
		}
	}

}
func (a *app) subtitlePreferencesChanged() {
	a.persist()
	a.cancelSubtitleJob()
	a.online = subtitleDownloadState{}
	if a.onlineOpen {
		a.findSubtitles(false, nil)

	}
}
func (a *app) subtitleClient() *subtitleService {
	if a.subtitleService == nil {
		a.subtitleService = newSubtitleService()
	}
	return a.subtitleService
}
func (a *app) subtitleCacheDir() string {
	if a.subtitlesDir != "" {
		return a.subtitlesDir
	}
	if a.cacheDir != "" {
		return filepath.Join(filepath.Dir(a.cacheDir), "subtitles")
	}
	return ""
}
func (a *app) findSubtitles(automatic bool, selected *subtitleTitle) {
	if a.window == nil || a.current < 0 || !a.status.Loaded {
		return
	}
	a.cancelSubtitleJob()
	a.online.Busy = true
	a.online.Error = ""
	a.online.Message = "Finding subtitles…"
	a.online.AutoChecked = true
	if selected == nil {
		a.online.Matches = nil
		a.online.Titles = nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	a.online.cancel = cancel
	request, file, language := a.subtitleRequest, a.queue[a.current], a.subtitleLanguage
	query := a.subtitleQuery
	fps := a.status.FPS
	client := a.subtitleClient()
	dir := a.subtitleCacheDir()
	window := a.window
	useCache := automatic && !a.onlineOpen
	var chosen *subtitleTitle
	if selected != nil {
		copy := *selected
		chosen = &copy
	}
	go func() {
		defer cancel()
		if useCache && chosen == nil {
			if cached, ok := subtitleCacheRead(dir, file, language); ok {
				window.Update(func() {
					if a.subtitleCurrent(request, file) {
						a.online.Busy = false
						a.attachDownloadedSubtitle(cached)
					}
				})
				return
			}
		}
		id := identifyVideo(file, query)
		var titles []subtitleTitle
		var title subtitleTitle
		var matches []onlineSubtitle
		var cached cachedSubtitle
		var err error
		if chosen == nil {
			titles, err = client.titles(ctx, id)
			if err == nil && len(titles) > 0 && (id.ID != "" || certainTitle(titles, id)) {
				title = titles[0]
			}
		} else {
			title = *chosen
			titles = []subtitleTitle{title}
		}
		if err == nil && title.ID != "" {
			matches, err = client.search(ctx, title, id, file, language, fps)
		}
		if err == nil && automatic && len(matches) > 0 {
			for i := 0; i < min(3, len(matches)); i++ {
				if matches[i].Score < 0 {
					break
				}
				cached, err = client.download(ctx, dir, file, language, matches[i])
				if err == nil {
					break
				}
				if ctx.Err() != nil || errors.Is(err, errSubtitleRateLimit) {
					break
				}
			}
		}
		if ctx.Err() != nil && err == nil {
			err = ctx.Err()
		}
		window.Update(func() {
			if !a.subtitleCurrent(request, file) {
				return
			}
			a.online.Busy = false
			a.online.Titles = titles
			a.online.Matches = matches
			a.online.Title = title
			if err != nil {
				a.online.Error = subtitleUserError(err)
				a.online.Message = ""
				return
			}
			switch {
			case cached.Path != "":
				a.attachDownloadedSubtitle(cached)
			case len(titles) == 0:
				a.online.Message = "No matching title found. Edit the title and search again."
			case title.ID == "":
				a.online.Message = "Choose the matching title."
			case len(matches) == 0:
				a.online.Message = "No " + language + " subtitles found. Try another language or title."
			default:
				a.online.Message = fmt.Sprintf("%d %s matches", len(matches), language)
			}
		})
	}()
}
func subtitleUserError(err error) string {
	if err == context.Canceled {
		return "Subtitle request cancelled."
	}
	if err == context.DeadlineExceeded {
		return "Subtitle search timed out. Try again."
	}
	return err.Error()
}
func (a *app) downloadSubtitle(sub onlineSubtitle) {
	if a.window == nil || a.current < 0 {
		return
	}
	a.cancelSubtitleJob()
	a.online.Busy = true
	a.online.Error = ""
	a.online.Message = "Downloading subtitles…"
	a.online.AutoChecked = true
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	a.online.cancel = cancel
	request, file, language := a.subtitleRequest, a.queue[a.current], a.subtitleLanguage
	window, client, dir := a.window, a.subtitleClient(), a.subtitleCacheDir()
	go func() {
		defer cancel()
		cached, err := client.download(ctx, dir, file, language, sub)
		window.Update(func() {
			if !a.subtitleCurrent(request, file) {
				return
			}
			a.online.Busy = false
			if err != nil {
				a.online.Error = subtitleUserError(err)
				a.online.Message = ""
				return
			}
			a.attachDownloadedSubtitle(cached)
		})
	}()
}
func (a *app) attachDownloadedSubtitle(cached cachedSubtitle) {
	title := "Online / " + cached.Language + " / " + cached.Title
	if len(title) > 180 {
		title = title[:180]
	}
	a.online.PendingTitle = title
	a.online.Message = "Loading " + cached.Language + " captions…"
	a.online.Error = ""
	a.online.AutoChecked = true
	a.send("sub-add", cached.Path, "select", title, languageCodes[cached.Language])
	a.onlineOpen = false
	a.captionsOpen = false
	a.layoutHost()
}
func (a *app) closeSubtitleSearch() {
	if a.online.Busy {
		a.online.Message = "Subtitle request cancelled."
	}
	a.cancelSubtitleJob()
	a.onlineOpen = false
	a.layoutHost()
}
func (a *app) subtitleStatus() string {
	if a.online.Error != "" {
		return a.online.Error
	}
	return strings.TrimSpace(a.online.Message)
}
func (a *app) subtitleQueryEdited() {
	a.cancelSubtitleJob()
	a.online.Titles = nil
	a.online.Matches = nil
	a.online.Title = subtitleTitle{}
	a.online.Message = ""
	a.online.Error = ""
}
