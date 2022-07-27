package dumper

import (
	"bytes"
	"fmt"
	"hlsdump/pkg/logger"
	"io"
	"net/http"
	"os"
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

type Config struct {
	Url             string
	Dir             string
	Retry           int
	Timeout         int
	RefreshInterval float64 // TargetDuration的倍数，可以是小数
}

type HlsDump struct {
	cfg Config
}

func New(cfg *Config) *HlsDump {
	dump := &HlsDump{
		cfg: *cfg,
	}
	return dump
}

func (hd *HlsDump) Start() {
	log := logger.Inst()

	var m3u8Reader io.Reader
	if strings.HasPrefix(hd.cfg.Url, "http") {
		tr := &http.Transport{
			Proxy:             http.ProxyFromEnvironment,
			DisableKeepAlives: false,
		}

		c := http.Client{
			Transport: tr,
			Timeout:   time.Duration(hd.cfg.Timeout) * time.Second,
		}

		resp, err := c.Get(hd.cfg.Url)
		if err != nil {
			log.Error(fmt.Sprintf("LoadPlaylist url:%s failed, err:%v", hd.cfg.Url, err))
			return
		}

		defer resp.Body.Close()
		m3u8Reader = resp.Body
	} else { // maybe a local file
		f, err := os.Open(hd.cfg.Url)
		if err != nil {
			log.Error(fmt.Sprintf("try to open %s failed, err:%v", hd.cfg.Url, err))
			return
		}
		m3u8Reader = f
	}

	m3u8Type := M3U8_TYPE_INVALID

	buf := bytes.Buffer{}
	io.Copy(&buf, m3u8Reader)
	body := buf.Bytes()
	lstart := 0
	for idx, c := range body {
		if c != '\n' {
			continue
		}
		line := string(body[lstart:idx])
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			m3u8Type = M3U8_TYPE_MASTER
			break
		} else if strings.HasPrefix(line, "#EXTINF") {
			m3u8Type = M3U8_TYPE_MEDIA
			break
		}
		lstart = idx + 1
	}

	if m3u8Type == M3U8_TYPE_INVALID {
		log.Error("invalid m3u8 file")
		return
	}

	var playlist Playlist
	if m3u8Type == M3U8_TYPE_MASTER {
		log.Info("It seems this is a master playlist, please dump each media playlist separately!")
		return

		/*
			baseurl := "."
			if pos := strings.LastIndex(hd.cfg.Url, "/"); pos > 0 {
				baseurl = hd.cfg.Url[:pos]
			} else if pos == 0 {
				baseurl = "/"
			}
			p, err := NewMasterPlaylist(hd.cfg.Logger, baseurl, &buf, hd.cfg.Dir)
			if err != nil {
				fmt.Printf("NewMasterPlaylist failed, err:%v\n", err)
				return
			}
			playlist = p
		*/
	}
	playlist = NewMediaPlaylist(&MediaPlaylistConfig{urlstr: hd.cfg.Url, dir: hd.cfg.Dir, retry: hd.cfg.Retry, timeout: hd.cfg.Timeout})
	playlist.Load()
}
