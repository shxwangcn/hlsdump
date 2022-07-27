package dumper

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"hlsdump/pkg/logger"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

type MediaPlaylistConfig struct {
	urlstr  string
	dir     string
	retry   int
	timeout int
}

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
	cfg            MediaPlaylistConfig
	tm             *TaskManager
	Uri            string
	DirUrl         string
	Dir            string
	client         *http.Client
	m3u8File       *os.File
	Master         *MasterPlaylist
	Seqno          int64 // next media sequence number
	TargetDuration int64
	Segments       []*SegmentInfo // only keep segments in latest m3u8(at least 5)
	faildCount     int
}

func NewMediaPlaylist(c *MediaPlaylistConfig) *MediaPlaylist {
	log := logger.Inst()
	if !strings.HasPrefix(c.urlstr, "http") {
		log.Warn("url is not http/https, do you want to dump a local media file??", zap.String("url", c.urlstr))
		return nil
	}

	urlobj, err := url.Parse(c.urlstr)
	if err != nil {
		log.Error("invalid url string", zap.String("url", c.urlstr), zap.Error(err))
		return nil
	}
	uri := urlobj.Path

	tr := &http.Transport{
		DisableKeepAlives:   false,
		MaxConnsPerHost:     3,
		MaxIdleConnsPerHost: 3,
		Proxy:               http.ProxyFromEnvironment,
	}
	httpc := &http.Client{
		Transport: tr,
		Timeout:   time.Duration(c.timeout) * time.Second,
	}

	dir := fmt.Sprintf("%s-%d", c.dir, time.Now().Unix())
	err = os.MkdirAll(dir, 0700)
	if err != nil {
		log.Error("mkdir failed", zap.String("dir", dir), zap.Error(err))
		return nil
	}

	dirPos := strings.LastIndex(c.urlstr, "/")
	dirUrl := c.urlstr[:dirPos]

	tm := NewTaskManager(c.retry, c.timeout)

	mp := &MediaPlaylist{
		cfg:    *c,
		Uri:    uri,
		DirUrl: dirUrl,
		Dir:    dir,
		client: httpc,
		tm:     tm,
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
	log := logger.Inst()

	log.Info("now start loading", zap.String("url", mp.cfg.urlstr), zap.String("output dir", mp.Dir))
	mp.load()
}

func (mp *MediaPlaylist) load() {
	log := logger.Inst()

	mp.tm.Run()

	m3u8File, err := os.OpenFile(mp.Dir+"/index.m3u8", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		log.Error("open index.m3u8 failed", zap.String("variant", mp.Uri), zap.Error(err))
		return
	}
	mp.m3u8File = m3u8File

	lastUpdatedTs := time.Now().UnixMilli()

	for {
		segments, err := mp.refreshPlaylist()
		m3u8File.Sync()
		if err != nil && err != io.EOF {
			log.Error("refresh playlist failed", zap.String("variant", mp.Uri), zap.Error(err))
			break
		}

		now := time.Now().UnixMilli()

		if len(segments) == 0 {
			stuckDuration := now - lastUpdatedTs
			log.Warn("playlist has been stuck for a while",
				zap.String("variant", mp.Uri),
				zap.Int64("duration(ms)", stuckDuration),
				zap.Int64("TargetDuration", mp.TargetDuration),
			)
		} else {
			lastUpdatedTs = now
		}

		for _, ts := range segments {
			log.Info("New segment found", zap.String("variant", mp.Uri),
				zap.String("name", ts.Name),
				zap.Float64("duration", ts.Duration),
				zap.String("uri", ts.URI),
			)

			mp.m3u8File.WriteString(fmt.Sprintf("##%s\n", ts.URI))
			mp.m3u8File.WriteString(ts.INF + "\n")
			mp.m3u8File.WriteString(fmt.Sprintf("%d.ts\n", ts.Seqno))

			// download ts and save it as file
			tsurl := ts.URI
			if !strings.HasPrefix(tsurl, "http") {
				tsurl = fmt.Sprintf("%s/%s", mp.DirUrl, tsurl)
			}
			filename := fmt.Sprintf("%s/%d.ts", mp.Dir, ts.Seqno)
			mp.tm.Push(&Task{url: tsurl, filename: filename})
		}

		if err == io.EOF {
			log.Info("load complete!", zap.String("variant", mp.Uri))
			break
		}

		sleep := mp.TargetDuration
		if sleep < 1 {
			sleep = 1
		}
		time.Sleep(time.Duration(sleep * int64(time.Second)))
	}

	mp.tm.Stop()
	m3u8File.Close()
	log.Info("load complete!")
}

func (mp *MediaPlaylist) refreshPlaylist() ([]*SegmentInfo, error) {
	log := logger.Inst()
	start := time.Now().UnixMilli()

	resp, err := mp.client.Get(mp.cfg.urlstr)
	if err != nil {
		return nil, err
	}
	end := time.Now().UnixMilli()
	if resp.StatusCode != 200 {
		log.Warn("refresh playlist failed",
			zap.String("variant", mp.Uri),
			zap.Int64("start_ts", start),
			zap.Int64("end_ts", end),
			zap.Int64("delay", end-start),
			zap.Int("StatusCode", resp.StatusCode),
		)
		mp.faildCount++
		if mp.faildCount > 3 {
			return nil, errors.New(resp.Status)
		}
	} else {
		mp.faildCount = 0
	}

	defer resp.Body.Close()

	bFirstLoad := false
	if mp.Seqno == 0 {
		bFirstLoad = true
	}

	segments := make([]*SegmentInfo, 0, 10)

	bDump := false

	buf := bytes.Buffer{}
	tee := io.TeeReader(resp.Body, &buf)
	scanner := bufio.NewScanner(tee)
	seqno := int64(0)
	totalCnt := 0
	for scanner.Scan() {
		line := scanner.Text()
		log.Info(fmt.Sprintf("line: %s", line))
		if bFirstLoad && strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#EXTINF") {
			mp.m3u8File.WriteString(line + "\n")
		}

		if line == "#EXT-X-ENDLIST" {
			return segments, io.EOF
		}

		if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			sn, err := strconv.ParseInt(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"), 10, 32)
			if err != nil {
				log.Error("invalid media seqnuence", zap.String("variant", mp.Uri), zap.String("data", line))
				return nil, errors.New("InvalidMediaSequence")
			}
			if sn > mp.Seqno {
				log.Error(fmt.Sprintf("media sequence number discontinuity, expected %d but got %d", mp.Seqno, sn), zap.String("variant", mp.Uri))
				bDump = true
			}
			seqno = sn
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			targetDuration, err := strconv.ParseInt(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"), 10, 32)
			if err != nil {
				log.Error("invalid target duration", zap.String("variant", mp.Uri), zap.String("data", line))
				return nil, errors.New("InvalidTargetDuration")
			}
			if mp.TargetDuration != 0 && targetDuration != mp.TargetDuration {
				log.Warn("TargetDuration changed", zap.String("variant", mp.Uri), zap.Int64("old", mp.TargetDuration), zap.Int64("new", targetDuration))
			}
			mp.TargetDuration = targetDuration
			continue
		}

		if strings.HasPrefix(line, "#EXTINF:") {
			totalCnt++
			infstr := strings.TrimSuffix(strings.TrimPrefix(line, "#EXTINF:"), ",")
			duration, err := strconv.ParseFloat(infstr, 32)
			if err != nil {
				log.Error("invalid segment duration", zap.String("variant", mp.Uri), zap.String("data", line))
				return nil, errors.New("InvalidMediaDuration")
			}
			if int64(math.Round(duration)) > mp.TargetDuration {
				log.Error("segment duration is larger than target duration", zap.String("variant", mp.Uri),
					zap.Float64("duration", duration),
					zap.Int64("TargetDuration", mp.TargetDuration),
				)
				return nil, errors.New("OverflowMediaDuration")
			}

			if !scanner.Scan() {
				log.Error("No URI for Segment", zap.String("variant", mp.Uri), zap.Int64("seqno", mp.Seqno))
				return nil, errors.New("MissingSegmentUri")
			}
			uri := scanner.Text()
			if seqno < mp.Seqno {
				log.Info("ignore old segment", zap.String("variant", mp.Uri), zap.Int64("seqno", seqno), zap.Int64("currentSeqNo", mp.Seqno))
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
	if bDump {
		log.Info("dump full playlist", zap.String("variant", mp.Uri), zap.String("data", buf.String()))
	}

	log.Info("refresh playlist done",
		zap.String("variant", mp.Uri),
		zap.Int("total segments", totalCnt),
		zap.Int("new segments", len(segments)),
		zap.Int64("start_ts", start),
		zap.Int64("end_ts", end),
		zap.Int64("delay", end-start),
	)
	return segments, nil
}
