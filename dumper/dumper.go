package dumper

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	M3U8_TYPE_INVALID = iota
	M3U8_TYPE_MASTER
	M3U8_TYPE_MEDIA
)

type Playlist interface {
	Version() int
	Type() int
	Load()
}

type HlsDump struct {
	Url string
	Dir string
}

func New(url string, dir string) *HlsDump {
	dump := &HlsDump{
		Url: url,
		Dir: dir,
	}
	return dump
}

func (hd *HlsDump) Start() {
	tr := &http.Transport{
		DisableKeepAlives: false,
	}

	c := http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}

	resp, err := c.Get(hd.Url)
	if err != nil {
		fmt.Printf("LoadPlaylist url:%s failed, err:%v\n", hd.Url, err)
		return
	}

	defer resp.Body.Close()

	m3u8Type := M3U8_TYPE_INVALID

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF") || strings.HasPrefix(line, "#EXT-X-MEDIA") {
			m3u8Type = M3U8_TYPE_MASTER
			break
		} else if strings.HasPrefix(line, "#EXTINF") {
			m3u8Type = M3U8_TYPE_MEDIA
			break
		}
	}

	if m3u8Type == M3U8_TYPE_INVALID {
		fmt.Printf("invalid m3u8 file\n")
		return
	}

	var playlist Playlist
	if m3u8Type == M3U8_TYPE_MASTER {
		playlist = NewMasterPlaylist(hd.Url, hd.Dir)
	} else {
		playlist = NewMediaPlaylist(hd.Url, "master", hd.Dir)
	}
	playlist.Load()
}
