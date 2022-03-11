package dumper

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type SegmentInfo struct {
	Seqno     int64
	Name      string
	Duration  float64
	Url       string
	AddedTime int64
	INF       string
	URI       string
}

type MediaPlaylist struct {
	Url            string
	Uri            string
	DirUrl         string
	Dir            string
	client         *http.Client
	m3u8File       *os.File
	Master         *MasterPlaylist
	Seqno          int64 // next media sequence number
	TargetDuration int64
	Segments       []*SegmentInfo // only keep segments in latest m3u8(at least 5)
}

func NewMediaPlaylist(urlstr string, uri string, dir string) *MediaPlaylist {
	tr := &http.Transport{
		DisableKeepAlives: false,
		Proxy:             http.ProxyFromEnvironment,
	}
	c := &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}

	dir = fmt.Sprintf("%s-%d", dir, time.Now().Unix())
	err := os.MkdirAll(dir, 0700)
	if err != nil {
		fmt.Printf("mkdir for %s failed, err:%v\n", dir, err)
		return nil
	}

	dirPos := strings.LastIndex(urlstr, "/")
	dirUrl := urlstr[:dirPos]

	mp := &MediaPlaylist{
		Url:    urlstr,
		Uri:    uri,
		DirUrl: dirUrl,
		Dir:    dir,
		client: c,
	}
	return mp
}

func (mp *MediaPlaylist) Version() int {
	return 0
}

func (mp *MediaPlaylist) Type() int {
	return M3U8_TYPE_MEDIA
}

func (mp *MediaPlaylist) Load() {
	fmt.Printf("target is a media playlist, now start loading!\n")
	mp.load()
}

func (mp *MediaPlaylist) load() {
	m3u8File, err := os.OpenFile(mp.Dir+"/index.m3u8", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		fmt.Printf("[%s] open index.m3u8 failed, err:%v\n", mp.Uri, err)
		return
	}
	mp.m3u8File = m3u8File

	for {
		segments, err := mp.refreshPlaylist()
		m3u8File.Sync()
		if err != nil && err != io.EOF {
			fmt.Printf("refresh playlist failed, err:%v\n", err)
			break
		}

		for _, ts := range segments {
			fmt.Printf("[%s] New segment found, name:%s, duration:%f, uri:%s\n", mp.Uri, ts.Name, ts.Duration, ts.URI)

			mp.m3u8File.WriteString(fmt.Sprintf("##%s\n", ts.URI))
			mp.m3u8File.WriteString(ts.INF + "\n")
			mp.m3u8File.WriteString(fmt.Sprintf("%d.ts\n", ts.Seqno))

			// download ts and save it as file
			tsurl := ts.URI
			if !strings.HasPrefix(tsurl, "http") {
				tsurl = fmt.Sprintf("%s/%s", mp.DirUrl, tsurl)
			}
			err = mp.downloadSegment(tsurl, ts.Seqno)
			if err != nil {
				fmt.Printf("[%s] download segment failed, seqno:%d, duration:%f, uri:%s\n", mp.Uri, mp.Seqno, ts.Duration, tsurl)
			}

		}

		if err == io.EOF {
			fmt.Printf("[%s] load complete!\n", mp.Uri)
			break
		}

		time.Sleep(time.Duration(mp.TargetDuration * int64(time.Second)))
	}

	m3u8File.Close()
}

func (mp *MediaPlaylist) refreshPlaylist() ([]*SegmentInfo, error) {
	resp, err := mp.client.Get(mp.Url)
	if err != nil {
		fmt.Printf("load master playlist failed, err:%v\n", err)
		return nil, err
	}

	defer resp.Body.Close()

	bFirstLoad := false
	if mp.Seqno == 0 {
		bFirstLoad = true
	}

	segments := make([]*SegmentInfo, 0, 10)

	scanner := bufio.NewScanner(resp.Body)
	seqno := int64(0)
	for scanner.Scan() {
		line := scanner.Text()
		if bFirstLoad && strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#EXTINF") {
			mp.m3u8File.WriteString(line + "\n")
		}

		if line == "#EXT-X-ENDLIST" {
			return nil, io.EOF
		}

		if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			sn, err := strconv.ParseInt(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"), 10, 32)
			if err != nil {
				fmt.Printf("[%s] invalid media seqnuence:%s\n", mp.Uri, line)
				return nil, errors.New("InvalidMediaSequence")
			}
			if sn > mp.Seqno {
				fmt.Printf("[%s] mediq sequence number discontinuity, expected %d but got %d\n", mp.Uri, mp.Seqno, seqno)
			}
			seqno = sn
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			targetDuration, err := strconv.ParseInt(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"), 10, 32)
			if err != nil {
				fmt.Printf("[%s] invalid target duration:%s\n", mp.Uri, line)
				return nil, errors.New("InvalidTargetDuration")
			}
			if mp.TargetDuration != 0 && targetDuration != mp.TargetDuration {
				fmt.Printf("[%s] TargetDuration changed from %d to %d\n", mp.Uri, mp.TargetDuration, targetDuration)
			}
			mp.TargetDuration = targetDuration
			continue
		}

		if strings.HasPrefix(line, "#EXTINF:") {
			infstr := strings.TrimSuffix(strings.TrimPrefix(line, "#EXTINF:"), ",")
			duration, err := strconv.ParseFloat(infstr, 32)
			if err != nil {
				fmt.Printf("[%s] invalid segment duration:%s\n", mp.Uri, line)
				return nil, errors.New("InvalidMediaDuration")
			}
			if int64(math.Round(duration)) > mp.TargetDuration {
				fmt.Printf("[%s] duration(%f) is larger than target duration(%d)\n", mp.Uri, duration, mp.TargetDuration)
				return nil, errors.New("OverflowMediaDuration")
			}

			if !scanner.Scan() {
				fmt.Printf("[%s] No URI for Segment, seqno:%d\n", mp.Uri, mp.Seqno)
				return nil, errors.New("MissingSegmentUri")
			}
			uri := scanner.Text()
			if seqno < mp.Seqno {
				fmt.Printf("[%s] ignore old segment, seqno:%d\n", mp.Uri, seqno)
				seqno++
				continue
			}
			seg := &SegmentInfo{
				Seqno:     seqno,
				Name:      fmt.Sprintf("%d.ts", seqno),
				Duration:  duration,
				AddedTime: time.Now().Unix(),
				URI:       uri,
				INF:       line,
			}
			segments = append(segments, seg)

			seqno++
			mp.Seqno = seqno
		}
	}
	return segments, nil
}

func (mp *MediaPlaylist) downloadSegment(uri string, seqno int64) error {
	resp, err := mp.client.Get(uri)
	if err != nil {
		fmt.Printf("[%s] download segment failed, uri:%s, err:%v\n", mp.Uri, uri, err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Printf("[%s] download %s failed, statuscode:%d\n", mp.Uri, uri, resp.StatusCode)
	}

	filename := fmt.Sprintf("%s/%d.ts", mp.Dir, seqno)
	tsFile, err := os.OpenFile(filename, os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		fmt.Printf("[%s] create segment file for %s failed\n", mp.Uri, filename)
	}
	_, err = io.Copy(tsFile, resp.Body)
	if err != nil {
		fmt.Printf("[%s] write segment to files failed, file:%s, err:%v\n", mp.Uri, filename, err)
		return err
	}

	return nil
}
