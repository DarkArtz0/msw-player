package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/egoist/mygo"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type videoFile struct{ Path, Folder, Name string }
type libraryRow struct {
	videoIndex int
	folder     string
	count      int
}

type preferences struct {
	GroupByFolder    bool                `json:"groupByFolder,omitempty"`
	Folders          []string            `json:"folders"`
	Dark             bool                `json:"dark"`
	Density          string              `json:"density,omitempty"`
	History          map[string]bookmark `json:"history,omitempty"`
	SubtitleLanguage string              `json:"subtitleLanguage,omitempty"`
	SubtitleStyle    *subtitleAppearance `json:"subtitleStyle,omitempty"`
}

func loadSettings(path string) (preferences, error) {
	var p preferences
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(b, &p)
	return p, err
}

type settingsStore struct {
	jobs chan preferences
	done chan struct{}
	mu   sync.Mutex
	err  error
	path string
}

func newSettingsStore(path string) *settingsStore {
	s := &settingsStore{path: path, jobs: make(chan preferences, 16), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		for p := range s.jobs {
			b, err := json.MarshalIndent(p, "", "  ")
			if err == nil {
				err = os.MkdirAll(filepath.Dir(path), 0755)
			}
			if err == nil {
				err = os.WriteFile(path+".tmp", b, 0600)
			}
			if err == nil {
				err = os.Rename(path+".tmp", path)
			}
			s.mu.Lock()
			s.err = err
			s.mu.Unlock()
		}
	}()
	return s
}
func (s *settingsStore) close()           { close(s.jobs); <-s.done }
func (s *settingsStore) lastError() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (a *app) persist() {
	a.invalidateWindows()
	if a.store != nil {
		history := make(map[string]bookmark, len(a.history))
		for path, b := range a.history {
			history[path] = b
		}
		style := a.subtitleStyle
		p := preferences{Folders: append([]string(nil), a.folders...), Dark: a.dark, Density: a.density, History: history, SubtitleLanguage: a.subtitleLanguage, SubtitleStyle: &style, GroupByFolder: a.groupByFolder}
		select {
		case a.store.jobs <- p:
		default:
			a.libraryNotice = "Settings are busy. Please try again."
		}
	}
}

func scanFolders(ctx context.Context, folders []string) ([]videoFile, []string) {
	var videos []videoFile
	var warnings []string
	seen := map[string]bool{}
	for _, folder := range folders {
		err := filepath.WalkDir(folder, func(path string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", path, err))
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !d.IsDir() && d.Type()&os.ModeSymlink == 0 && supported(path) {
				key := strings.ToLower(filepath.Clean(path))
				if !seen[key] {
					seen[key] = true
					videos = append(videos, videoFile{Path: path, Folder: folder, Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))})
				}
			}
			return nil
		})
		if err != nil && ctx.Err() == nil {
			warnings = append(warnings, err.Error())
		}
	}
	sort.Slice(videos, func(i, j int) bool {
		left, right := strings.ToLower(videos[i].Name), strings.ToLower(videos[j].Name)
		if left == right {
			return videos[i].Path < videos[j].Path
		}
		return left < right
	})
	return videos, warnings
}
func (a *app) rescan() {
	if a.window == nil {
		return
	}
	if a.scanCancel != nil {
		a.scanCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.scanCancel = cancel
	a.scanGeneration++
	generation := a.scanGeneration
	a.scanning = true
	folders := append([]string(nil), a.folders...)
	window := a.window
	go func() {
		videos, warnings := scanFolders(ctx, folders)
		if ctx.Err() != nil {
			return
		}
		window.Update(func() {
			if generation != a.scanGeneration {
				return
			}
			a.library = videos
			a.thumbs = map[string]*thumbnail{}
			a.scanning = false
			a.libraryNotice = ""
			if len(warnings) > 0 {
				a.libraryNotice = fmt.Sprintf("Some folders could not be read (%d). Check that your sources are available.", len(warnings))
			}
			a.filterLibrary()
			a.invalidateWindows()
		})
	}()
}
func (a *app) filterLibrary() {
	a.filtered = nil
	q := strings.ToLower(strings.TrimSpace(a.search))
	for _, v := range a.library {
		if a.folderFilter != "" {
			rel, err := filepath.Rel(a.folderFilter, v.Path)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
		}
		if q != "" && !strings.Contains(strings.ToLower(v.Name), q) {
			continue
		}
		a.filtered = append(a.filtered, v)
	}
	if a.groupByFolder {
		sort.SliceStable(a.filtered, func(i, j int) bool {
			return strings.ToLower(filepath.Dir(a.filtered[i].Path)) < strings.ToLower(filepath.Dir(a.filtered[j].Path))
		})
	}
	a.libraryRows = nil
	header := -1
	for i, v := range a.filtered {
		folder := filepath.Dir(v.Path)
		if a.groupByFolder {
			if header < 0 || !strings.EqualFold(a.libraryRows[header].folder, folder) {
				header = len(a.libraryRows)
				a.libraryRows = append(a.libraryRows, libraryRow{videoIndex: -1, folder: folder})
			}
			a.libraryRows[header].count++
		}
		a.libraryRows = append(a.libraryRows, libraryRow{videoIndex: i})
	}
}
func (a *app) addFolder(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		a.libraryNotice = err.Error()
		return
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		a.libraryNotice = "Choose an available folder."
		return
	}
	for _, v := range a.folders {
		if strings.EqualFold(v, abs) {
			return
		}
	}
	a.folders = append(a.folders, abs)
	a.persist()
	a.rescan()
}
func (a *app) browseFolder() {
	if a.window == nil {
		return
	}
	window := a.window
	parent := window
	if a.settingsWindow != nil {
		parent = a.settingsWindow
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: "Add video folder", Directory: true, Multiple: true})
		window.Update(func() {
			if err != nil {
				a.libraryNotice = err.Error()
				return
			}
			for _, path := range paths {
				a.addFolder(path)
			}
		})
	}()
}

func (a *app) importPaths(paths []string) {
	if a.window == nil {
		return
	}
	window := a.window
	paths = append([]string(nil), paths...)
	go func() {
		var folders, files []string
		for _, path := range paths {
			info, err := os.Stat(path)
			if err == nil && info.IsDir() {
				folders = append(folders, path)
			} else {
				files = append(files, path)
			}
		}
		window.Update(func() {
			for _, folder := range folders {
				a.addFolder(folder)
			}
			if len(files) > 0 {
				a.add(files)
			}
		})
	}()
}
func (a *app) removeFolder(path string) {
	for i, v := range a.folders {
		if v == path {
			a.folders = append(a.folders[:i], a.folders[i+1:]...)
			break
		}
	}
	if a.folderFilter == path {
		a.folderFilter = ""
	}
	a.persist()
	a.rescan()
}
func (a *app) playLibrary(index int) {
	if index < 0 || index >= len(a.filtered) {
		return
	}
	a.checkpoint(true)
	a.status.Loaded = false
	a.queue = make([]string, len(a.filtered))
	for i, v := range a.filtered {
		a.queue[i] = v.Path
	}
	a.play(index)
}
func (a *app) navigate(page string) {
	a.checkpoint(true)
	// A playing video keeps playing in picture-in-picture.
	if a.page == "player" && page != "player" {
		if a.playing() {
			a.pip = true
		} else if !a.pip {
			a.send("set_property", "pause", true)
		}
	}
	if page == "player" {
		a.pip = false
	}
	a.transition = nil
	a.page = page
	a.showQueue = false
	a.captionsOpen = false
	a.audioOpen = false
	a.chaptersOpen = false
	a.onlineOpen = false
	a.statsOpen = false
	a.layoutHost()
}
func (a *app) toggleFullscreen() {
	if a.current < 0 {
		return
	}
	if a.transition != nil {
		a.transition = nil
		a.send("set_property", "pause", false)
	}
	a.page = "player"
	a.pip = false
	a.showQueue = false
	a.captionsOpen = false
	a.audioOpen = false
	a.chaptersOpen = false
	a.onlineOpen = false
	a.statsOpen = false
	a.fullscreen = !a.fullscreen
	a.controlsCursorKnown = false
	if !a.fullscreen {
		a.hideFullscreenControls()
	}
	if a.window != nil {
		a.window.SetFullScreen(a.fullscreen)
		a.window.Invalidate()
	}
	a.layoutHost()
}
func (a *app) exitFullscreen() {
	a.fullscreen = false
	a.hideFullscreenControls()
	if a.window != nil {
		a.window.SetFullScreen(false)
		a.window.Invalidate()
	}
	a.layoutHost()
}
