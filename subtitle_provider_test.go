package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/egoist/mygo/ui"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testSRT = "1\n00:00:01,000 --> 00:00:03,000\nIn-app captions\n"

func TestVideoIdentificationAndRanking(t *testing.T) {
	for _, sample := range []struct {
		file, query, kind, year string
		season, episode         int
	}{
		{"The.Matrix.1999.1080p.BluRay.mkv", "The Matrix", "movie", "1999", 0, 0},
		{"The.Diplomat.S01E03.720p.WEBRip.mkv", "The Diplomat", "series", "", 1, 3},
		{"Show.Name.2x04.mkv", "Show Name", "series", "", 2, 4},
		{"1917.2019.1080p.mkv", "1917", "movie", "2019", 0, 0},
		{"1984.mkv", "1984", "movie", "", 0, 0},
		{"2001.A.Space.Odyssey.1968.1080p.mkv", "2001 A Space Odyssey", "movie", "1968", 0, 0},
	} {
		id := identifyVideo(sample.file, "")
		if id.Query != sample.query || id.Type != sample.kind || id.Year != sample.year || id.Season != sample.season || id.Episode != sample.episode {
			t.Fatalf("%s: %+v", sample.file, id)
		}
	}
	id := identifyVideo("Film.2020.mkv", "")
	titles := []subtitleTitle{{ID: "tt1234567", Name: "Film", Year: "2020"}, {ID: "tt1234568", Name: "Film", Year: "1984"}}
	if !certainTitle(titles, id) {
		t.Fatal("year did not resolve title")
	}
	id.Year = ""
	if certainTitle(titles, id) {
		t.Fatal("ambiguous remake was auto-selected")
	}
	if !sameLanguage("en", "English") || !sameLanguage("fra", "French") || sameLanguage("eng", "French") {
		t.Fatal("language aliases")
	}
	exact := onlineSubtitle{Release: "Film.2020.1080p.BluRay.x264", FPS: 23976}
	cam := onlineSubtitle{Release: "Film.2020.TS.XviD", FPS: 25000}
	if rankSubtitle(exact, "Film.2020.1080p.BluRay.x264.mkv", 23.976) <= rankSubtitle(cam, "Film.2020.1080p.BluRay.x264.mkv", 23.976) {
		t.Fatal("bad release ranking")
	}
}
func TestSubtitleSearchDownloadAndCache(t *testing.T) {
	var server *httptest.Server
	searches, downloads := 0, 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/catalog/"):
			json.NewEncoder(w).Encode(map[string]any{"metas": []subtitleTitle{{ID: "tt1234567", Name: "Film", Year: "2020"}, {ID: "tt1234568", Name: "Film", Year: "1984"}}})
		case strings.HasPrefix(r.URL.Path, "/subtitles/"):
			searches++
			if !strings.Contains(r.URL.Path, "videoHash=") || !strings.Contains(r.URL.Path, "filename=") {
				t.Error("missing file matching extras")
			}
			json.NewEncoder(w).Encode(map[string]any{"subtitles": []onlineSubtitle{{ID: "wrong", Language: "fre", URL: server.URL + "/sub"}, {ID: "cam", Language: "eng", Release: "Film.2020.TS.XviD", URL: server.URL + "/cam"}, {ID: "best", Language: "eng", Release: "Film.2020.1080p.BluRay.x264", FPS: 23976, URL: server.URL + "/sub"}}})
		case r.URL.Path == "/sub":
			downloads++
			w.Header().Set("Content-Type", "application/x-subrip")
			fmt.Fprint(w, testSRT)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newSubtitleService()
	client.metadata = server.URL
	client.provider = server.URL
	client.testHTTP = true
	root := t.TempDir()
	file := filepath.Join(root, "Film.2020.1080p.BluRay.x264.mkv")
	os.WriteFile(file, make([]byte, 131072), 0600)
	dir := filepath.Join(root, "subtitles")
	id := identifyVideo(file, "")
	titles, err := client.titles(context.Background(), id)
	if err != nil || !certainTitle(titles, id) {
		t.Fatalf("%v %v", titles, err)
	}
	matches, err := client.search(context.Background(), titles[0], id, file, "English", 23.976)
	if err != nil || len(matches) != 2 || matches[0].ID != "best" {
		t.Fatalf("%+v %v", matches, err)
	}
	cached, err := client.download(context.Background(), dir, file, "English", matches[0])
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cached.Path)
	if !strings.Contains(string(data), "In-app captions") {
		t.Fatal("download missing")
	}
	read, ok := subtitleCacheRead(dir, file, "English")
	if !ok || read.Path != cached.Path || searches != 1 || downloads != 1 {
		t.Fatalf("cache %+v %v", read, ok)
	}
	os.WriteFile(file, []byte("changed"), 0600)
	if _, ok := subtitleCacheRead(dir, file, "English"); ok {
		t.Fatal("cache survived source change")
	}
}
func TestSubtitleArchivesAndValidation(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, name := range []string{"Show.S01E02.srt", "../../Show.S01E03.srt"} {
		f, _ := writer.Create(name)
		fmt.Fprint(f, strings.ReplaceAll(testSRT, "In-app captions", name))
	}
	writer.Close()
	data, ext, err := unpackSubtitle(archive.Bytes(), "Show.S01E03.mkv")
	if err != nil || ext != ".srt" || !strings.Contains(string(data), "S01E03") {
		t.Fatalf("%s %v", data, err)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	gz.Write([]byte(testSRT))
	gz.Close()
	if _, _, err := unpackSubtitle(compressed.Bytes(), "Film.mkv"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{[]byte("<html>Login required</html>"), []byte("Download limit exceeded"), make([]byte, (4<<20)+1)} {
		if _, _, err := unpackSubtitle(bad, "Film.mkv"); err == nil {
			t.Fatal("accepted invalid download")
		}
	}
	if _, err := readBounded(strings.NewReader("too long"), 3); err == nil {
		t.Fatal("unbounded response")
	}
	client := newSubtitleService()
	for _, address := range []string{"http://subs5.strem.io/file", "https://evil.example/file", "file:///C:/secret"} {
		if _, err := client.get(context.Background(), address, 1024); err == nil {
			t.Fatal("accepted untrusted URL")
		}
	}
}
func TestSubtitleRateLimitAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/limited" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	client := newSubtitleService()
	client.testHTTP = true
	if _, err := client.get(context.Background(), server.URL+"/limited", 1024); !errors.Is(err, errSubtitleRateLimit) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.get(ctx, server.URL+"/wait", 1024); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestCaptionsRequireUserRequest(t *testing.T) {
	a := newApp()
	a.current = 0
	a.queue = []string{"Film.mkv"}
	a.engine = &player{commands: make(chan []any, 16), cancel: func() {}}
	a.status = playback{Loaded: true, TrackJSON: `[{"id":2,"type":"sub","lang":"eng"},{"id":3,"type":"sub","lang":"fra"}]`}
	a.subtitleTick()
	if len(a.engine.commands) != 0 || a.online.AutoChecked || a.subtitleService != nil {
		t.Fatal("playback started automatic subtitle work")
	}
	a.chooseSubtitle("no")
	before := len(a.engine.commands)
	a.subtitleTick()
	if len(a.engine.commands) != before {
		t.Fatal("automatic job undid user's Off choice")
	}
	request := a.subtitleRequest
	path := a.queue[0]
	a.cancelSubtitleJob()
	if a.subtitleCurrent(request, path) {
		t.Fatal("stale response accepted")
	}
	cached := cachedSubtitle{Path: "C:/cached.srt", Title: "Film English", Language: "English"}
	a.attachDownloadedSubtitle(cached)
	last := []any{}
	for len(a.engine.commands) > 0 {
		last = <-a.engine.commands
	}
	if last[0] != "sub-add" || last[2] != "select" || last[4] != "eng" {
		t.Fatal(last)
	}
	a.status.TrackJSON = fmt.Sprintf(`[{"id":9,"type":"sub","external":true,"title":%q}]`, a.online.PendingTitle)
	a.subtitleTick()
	if a.online.PendingTitle != "" || !strings.Contains(a.online.Message, "loaded") {
		t.Fatal(a.online)
	}
}
func TestSubtitleProviderLive(t *testing.T) {
	if os.Getenv("MSWPLAYR_SUBTITLE_LIVE") == "" {
		t.Skip("optional live provider check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := newSubtitleService()
	file := "Interstellar.2014.1080p.BluRay.x264.mkv"
	id := identifyVideo(file, "")
	titles, err := client.titles(ctx, id)
	if err != nil || !certainTitle(titles, id) || titles[0].ID != "tt0816692" {
		t.Fatalf("%+v %v", titles, err)
	}
	matches, err := client.search(ctx, titles[0], id, file, "English", 23.976)
	if err != nil || len(matches) == 0 {
		t.Fatalf("matches %d %v", len(matches), err)
	}
	cached, err := client.download(ctx, t.TempDir(), file, "English", matches[0])
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cached.Path)
	if err != nil || len(data) < 1000 {
		t.Fatalf("download %d bytes %v", len(data), err)
	}
	t.Logf("Identified %s (%s), %d English results; downloaded %d bytes", titles[0].Name, titles[0].Year, len(matches), len(data))
	if os.Getenv("MSWPLAYR_INTEGRATION") != "" {
		video := filepath.Join(t.TempDir(), "sample.mkv")
		out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24", "-t", "4", "-c:v", "libx264", video).CombinedOutput()
		if err != nil {
			t.Fatalf("%v %s", err, out)
		}
		p := newPlayer(0)
		defer p.close()
		p.command("loadfile", video, "replace")
		wait := func(check func(playback) bool) {
			t.Helper()
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				s := p.snapshot()
				if s.Error != "" {
					t.Fatal(s.Error)
				}
				if check(s) {
					return
				}
				time.Sleep(40 * time.Millisecond)
			}
			t.Fatalf("player timeout: %+v", p.snapshot())
		}
		wait(func(s playback) bool { return s.Loaded })
		p.command("sub-add", cached.Path, "select", "Online download test", "eng")
		wait(func(s playback) bool {
			for _, track := range subtitleTracks(s.TrackJSON) {
				if track.External && track.Title == "Online download test" && s.SubtitleID == fmt.Sprint(track.ID) {
					return true
				}
			}
			return false
		})
		t.Log("Downloaded subtitle loaded and selected in mpv")
	}
}

func TestInAppSubtitleDialog(t *testing.T) {
	a := newApp()
	a.dark = true
	a.page = "player"
	a.current = 0
	a.queue = []string{"Film.2020.mkv"}
	a.status.Loaded = true
	a.onlineOpen = true
	a.subtitleQuery = "Film 2020"
	a.online.Title = subtitleTitle{ID: "tt1234567", Name: "Film"}
	a.online.Message = "3 English matches"
	for i := 0; i < 3; i++ {
		a.online.Matches = append(a.online.Matches, onlineSubtitle{ID: fmt.Sprint(i), Release: fmt.Sprintf("Film.2020.1080p.BluRay.Release-%d", i), Language: "eng"})
	}
	tt := ui.NewTester(a.view, 760, 540)
	if !tt.HasText("Find and load best") || !tt.HasText("Download and load") || tt.HasText("browser") {
		t.Fatal(tt.Texts())
	}
	bounds, ok := tt.Find("Download 2")
	if !ok || bounds.Y+bounds.H > 540 {
		t.Fatal(bounds)
	}
	if os.Getenv("MSWPLAYR_RENDER") != "" {
		os.MkdirAll("build", 0755)
		f, err := os.Create("build/subtitle-download-dialog.png")
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, tt.Image())
		f.Close()
	}
	if err := tt.Click("Close subtitle search"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.onlineOpen {
		t.Fatal("dialog did not close")
	}
}
