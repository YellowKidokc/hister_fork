// SPDX-License-Identifier: AGPL-3.0-or-later

package youtubemedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const maxBytes = 250 << 20

var (
	publishMu sync.Mutex
	videoID   = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	slots     = make(chan struct{}, 1)
	ErrBusy   = errors.New("another YouTube download is in progress")
)

func WatchURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Port() != "" {
		return ""
	}

	var id string
	switch strings.ToLower(u.Hostname()) {
	case "youtu.be":
		id = strings.Trim(u.Path, "/")
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "www.youtube-nocookie.com":
		if u.Path == "/watch" {
			id = u.Query().Get("v")
		} else {
			prefix, value, found := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
			if found && (prefix == "shorts" || prefix == "embed" || prefix == "live") {
				id = strings.TrimSuffix(value, "/")
			}
		}
	}
	if !videoID.MatchString(id) {
		return ""
	}
	return "https://www.youtube.com/watch?v=" + id
}

func FilePath(base, documentID string) string {
	key := sha256.Sum256([]byte(documentID))
	return filepath.Join(base, "youtube-media", hex.EncodeToString(key[:])+".mp4")
}

func Remove(base, documentID string) error {
	publishMu.Lock()
	defer publishMu.Unlock()
	if err := os.Remove(FilePath(base, documentID)); !os.IsNotExist(err) {
		return err
	}
	return nil
}

func Download(ctx context.Context, binary, path, rawURL string, stillIndexed func() bool) error {
	watchURL := WatchURL(rawURL)
	if watchURL == "" {
		return errors.New("expected a YouTube video URL")
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		return ErrBusy
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return errors.New("could not check the media folder")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("could not create the media folder")
	}
	temporary, err := os.MkdirTemp(dir, ".downloading-")
	if err != nil {
		return errors.New("could not prepare the media download")
	}
	defer os.RemoveAll(temporary)
	output := filepath.Join(temporary, "video.mp4")

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "--ignore-config", "--no-cache-dir", "--no-playlist",
		"--format", "best[ext=mp4][height<=720][vcodec^=avc1][acodec^=mp4a]", "--max-filesize", "250M",
		"--socket-timeout", "20", "--retries", "2", "--no-progress", "--output", output, "--", watchURL)
	command.WaitDelay = 2 * time.Second
	if err := command.Run(); err != nil {
		return errors.New("yt-dlp could not download a compatible YouTube video")
	}
	info, err := os.Stat(output)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 12 || info.Size() > maxBytes {
		return errors.New("no compatible MP4 file was downloaded within the 250 MiB limit")
	}
	publishMu.Lock()
	defer publishMu.Unlock()
	if !stillIndexed() {
		return errors.New("the document was deleted before the download finished")
	}
	if err := os.Rename(output, path); err != nil {
		return errors.New("could not save the downloaded video")
	}
	return nil
}
