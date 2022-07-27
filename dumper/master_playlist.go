package dumper

import (
	"bufio"
	"errors"
	"fmt"
	"hlsdump/pkg/logger"
	"io"
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
	BaseUrl  string
	Dir      string
	Variants []*VariantInfo
}

func NewMasterPlaylist(baseurl string, body io.Reader, dir string) (*MasterPlaylist, error) {
	log := logger.Inst()
	dirUrl := baseurl

	dir = fmt.Sprintf("%s-%d", dir, time.Now().Unix())

	err := os.MkdirAll(dir, 0700)
	if err != nil {
		log.Error(fmt.Sprintf("mkdir for %s failed, err:%v", dir, err))
		return nil, errors.New("MakeDirFailed")
	}

	m3u8File, err := os.OpenFile(dir+"/index.m3u8", os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		fmt.Printf("open %s/index.m3u8 failed, err:%v\n", dir, err)
		return nil, errors.New("OpenIndexFileFailed")
	}

	variants := make([]*VariantInfo, 0, 10)

	scanner := bufio.NewScanner(body)
	idx := 0
	for scanner.Scan() {
		line := scanner.Text()
		m3u8File.WriteString(line + "\n")
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			attrstr := strings.TrimPrefix(line, "#EXT-X-STREAM-INF:")
			if !scanner.Scan() {
				fmt.Printf("No URI for Variant(%s)\n", attrstr)
				return nil, errors.New("NoUriForVariant")
			}
			uri := scanner.Text()
			fmt.Printf("found new variant, uri:%s, attrs:%s\n", uri, attrstr)

			furi := uri
			if !strings.HasPrefix(uri, "http") {
				furi = baseurl + "/" + uri
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
					return nil, errors.New("InvalidBandwidth")
				}
			} else {
				return nil, errors.New("BandwidthNotFound")
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

			m3u8File.WriteString(fmt.Sprintf("%s/index.m3u8\n", name))

			media := NewMediaPlaylist(&MediaPlaylistConfig{urlstr: furi, dir: dir + "/" + name})

			v.Playlist = media
			variants = append(variants, v)

			idx++
		}
	}

	m3u8File.Sync()
	m3u8File.Close()
	mp := &MasterPlaylist{
		BaseUrl:  dirUrl,
		Dir:      dir,
		Variants: variants,
	}
	return mp, nil
}

func (mp *MasterPlaylist) Version() int {
	return 0
}

func (mp *MasterPlaylist) Type() int {
	return M3U8_TYPE_MASTER
}

func (mp *MasterPlaylist) Load() {
	log := logger.Inst()

	log.Info("target is a master playlist, now start loading!")

	wg := sync.WaitGroup{}

	for _, v := range mp.Variants {
		wg.Add(1)
		go func(v *VariantInfo) {
			defer wg.Done()
			v.Playlist.Load()
		}(v)
	}

	wg.Wait()
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
			if epos > 0 {
				value = attrstr[bpos : bpos+epos]
			} else {
				value = attrstr[bpos:]
			}
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

	return nil
}
