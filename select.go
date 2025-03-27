package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/as/hls"
)

var maxTime = time.Unix(1<<63-62135596801, 999999999)
var minTime = time.Unix(0, 0)

func parseSelectExpr(s string) (ts, te time.Time) {
	s = strings.ReplaceAll(s, "(", "")
	s = strings.ReplaceAll(s, ")", "")
	i := strings.IndexAny(s, "-+")
	if i < 0 {
		return parseUNIX(s), maxTime
	}
	if s[:i] != "" {
		ts = parseUNIX(s[:i])
	}
	if i+1 >= len(s) {
		return ts, maxTime
	}
	if s[i] == '+' {
		return ts, ts.Add(parseDur(s[i+1:]))
	}
	if s[i] == '-' || s[i] == ',' {
		return ts, parseUNIX(s[i+1:])
	}
	panic(fmt.Sprintf("unparseable expression: %s", s))
}

func parseDur(s string) time.Duration {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, "s") {
		s += "s"
	}
	d, _ := time.ParseDuration(s)
	return d
}
func parseUNIX(s string) time.Time {
	s = strings.TrimSpace(s)
	sec := int64(0)
	_, err := fmt.Sscan(strings.TrimSpace(s), &sec)
	if err != nil {
		return minTime
	}
	return time.Unix(sec, 0)
}

func group(m *hls.Master, id string) *hls.MediaInfo {
	if id == "" {
		return nil
	}
	for i := range m.Media {
		if m.Media[i].Group == id {
			return &m.Media[i]
		}
	}
	return nil
}

func quantifyV(s *hls.StreamInfo) (q int) {
	bw := s.Bandwidth
	if bw == 0 {
		bw = s.BandwidthAvg
	}
	if bw == 0 {
		bw = 1
	}
	pix := s.Resolution.X * s.Resolution.Y
	return bw * pix
}

func selectBest(m *hls.Master) (v *hls.Media, a *hls.Media) {
	if len(m.Stream) == 0 {
		panic("master playlist has no streams")
	}
	best, bestq := 0, 0
	for i := range m.Stream {
		q := quantifyV(&m.Stream[i])
		if q > bestq {
			best = i
		}
	}
	si := &m.Stream[best]
	parent := m.Path("")
	v = mediaPlaylist(si.Path(parent))
	if mi := group(m, si.Audio); mi != nil {
		return v, mediaPlaylist(mi.Path(parent))
	}
	return v, v
}

func selectrange(s, e time.Time, m *hls.Media) (p, q int) {
	p, now := findtime(0, minTime, s, m)
	q, now = findtime(p, now, e, m)
	return p, q
}

func findtime(n int, now, t time.Time, m *hls.Media) (int, time.Time) {
	for ; n < len(m.File); n++ {
		now = timeof(now, m, n)
		if !now.Add(m.File[n].Duration(0) / 2).Before(t) {
			break
		}
	}
	return n, now
}

func timeof(prev time.Time, m *hls.Media, i int) (after time.Time) {
	t := m.File[i].Time
	if !t.IsZero() {
		return t
	}
	if i > 0 {
		return prev.Add(m.File[i-1].Duration(0))
	}
	return prev
}
