package dumper

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type MediaInfo struct {
}

type MediaGroup struct {
	GroupId string
}

type VariantInfo struct {
	Url          string
	Bandwidth    int64
	AvgBandwidth int64
	Codecs       string
	Resolution   string
	FrameRate    float64
	AudioGroup   string
	VideoGroup   string
	Playlist     *MediaPlaylist
}

type MasterPlaylist struct {
	Url      string
	DirUrl   string
	Dir      string
	client   *http.Client
	Variants []*VariantInfo
	m3u8File *os.File
}

func NewMasterPlaylist(urlstr string, dir string) *MasterPlaylist {
	tr := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DisableKeepAlives: false,
	}

	c := &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}

	dirPos := strings.LastIndex(urlstr, "/")
	dirUrl := urlstr[:dirPos]

	dir = fmt.Sprintf("%s-%d", dir, time.Now().Unix())

	err := os.MkdirAll(dir, 0700)
	if err != nil {
		fmt.Printf("mkdir for %s failed, err:%v\n", dir, err)
		return nil
	}

	mp := &MasterPlaylist{
		Url:      urlstr,
		DirUrl:   dirUrl,
		Dir:      dir,
		client:   c,
		Variants: make([]*VariantInfo, 0, 10),
	}
	return mp
}

func (mp *MasterPlaylist) Version() int {
	return 0
}

func (mp *MasterPlaylist) Type() int {
	return M3U8_TYPE_MASTER
}

func (mp *MasterPlaylist) Load() {
	resp, err := mp.client.Get(mp.Url)
	if err != nil {
		fmt.Printf("load master playlist failed, err:%v\n", err)
		return
	}

	defer resp.Body.Close()

	m3u8File, err := os.OpenFile(mp.Dir+"/index.m3u8", os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		fmt.Printf("open %s/index.m3u8 failed, err:%v\n", mp.Dir, err)
		return
	}
	mp.m3u8File = m3u8File
	fmt.Printf("target is a master playlist, now start loading!\n")

	wg := sync.WaitGroup{}

	scanner := bufio.NewScanner(resp.Body)
	idx := 0
	for scanner.Scan() {
		line := scanner.Text()
		m3u8File.WriteString(line + "\n")
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			attrs := strings.TrimPrefix(line, "#EXT-X-STREAM-INF:")
			if !scanner.Scan() {
				fmt.Printf("No URI for Variant(%s)\n", attrs)
				return
			}
			uri := scanner.Text()
			mp.addNewVariant(&wg, uri, idx, attrs)
			idx++
		}
	}
	mp.m3u8File.Sync()

	wg.Wait()
	mp.m3u8File.Close()
}

// attr: PROGRAM-ID=1,BANDWIDTH=246440,CODECS="mp4a.40.5,avc1.42000d",RESOLUTION=320x184,NAME="240"
func parseHlsAttrList(attrstr string) map[string]string {
	attrs := make(map[string]string, 10)

	bpos := 0
	for { // 当找不到下一个kv分隔符时结束
		sepidx := strings.Index(attrstr[bpos:], "=")
		key := attrstr[bpos : bpos+sepidx]
		bpos += sepidx + 1
		epos := 0
		value := ""
		if attrstr[bpos] == '"' {
			bpos++
			epos = strings.Index(attrstr[bpos:], string([]byte{'"'}))
			if epos == -1 {
				return nil
			}
			value = attrstr[bpos : bpos+epos]
			bpos += epos + 1
			epos = strings.Index(attrstr[bpos:], ",")
		} else {
			epos = strings.Index(attrstr[bpos:], ",")
			value = attrstr[bpos : bpos+epos]
		}
		bpos += epos + 1
		attrs[key] = value
		if epos == -1 {
			break
		}
	}
	return attrs
}

func (mp *MasterPlaylist) addNewVariant(wg *sync.WaitGroup, uri string, idx int, attrstr string) error {
	fmt.Printf("found new variant, uri:%s, attrs:%s\n", uri, attrstr)

	furi := uri
	if !strings.HasPrefix(uri, "http") {
		furi = mp.DirUrl + "/" + uri
	}

	attrs := parseHlsAttrList(attrstr)

	v := &VariantInfo{
		Url:        furi,
		Codecs:     attrs["CODECS"],
		Resolution: attrs["RESOLUTION"],
		AudioGroup: attrs["AUDIO"],
		VideoGroup: attrs["VIDEO"],
	}

	if bandwidth, ok := attrs["BANDWIDTH"]; ok {
		v.Bandwidth, _ = strconv.ParseInt(bandwidth, 10, 64)
		if v.Bandwidth == 0 {
			return errors.New("InvalidBandwidth")
		}
	} else {
		return errors.New("BandwidthNotFound")
	}

	if avgbandwidth, ok := attrs["AVERAGE-BANDWIDTH"]; ok {
		v.AvgBandwidth, _ = strconv.ParseInt(avgbandwidth, 10, 64)
	}

	if framerate, ok := attrs["FRAME-RATE"]; ok {
		v.FrameRate, _ = strconv.ParseFloat(framerate, 32)
	}

	name := fmt.Sprintf("%d-%d", idx, v.Bandwidth)
	resolution, ok := attrs["RESOLUTION"]
	if ok {
		name += "-" + resolution[:strings.Index(resolution, "x")]
	}

	mp.m3u8File.WriteString(fmt.Sprintf("%s/index.m3u8\n", name))

	media := NewMediaPlaylist(furi, uri, mp.Dir+"/"+name)

	v.Playlist = media
	mp.Variants = append(mp.Variants, v)

	wg.Add(1)
	go func() {
		defer wg.Done()
		media.Load()
	}()

	return nil
}
