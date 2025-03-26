package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func makeBlackoutTS(av *Info, maxdur time.Duration) ([]byte, error) {
	if maxdur == 0 {
		maxdur, _ = time.ParseDuration(fmt.Sprintf("%ss", av.Dur))
	}
	if maxdur == 0 {
		maxdur = 12 * time.Second
	}
	ff := "ffmpeg -hide_banner -v quiet "
	iv := fmt.Sprintf("-f lavfi -i color=black:s=%dx%d:r=%f ", av.Video.Width, av.Video.Height, av.Video.FPS)
	ia := "-f lavfi -i anullsrc "
	ov := fmt.Sprintf("-pix_fmt yuv420p -r %f -c:v %s -enc_time_base 1/12800 -profile %s -level %s -b:v %d ",
		av.Video.FPS, av.Video.Codec, av.Video.Profile, av.Video.Level, av.Video.Bitrate.BPS)
	oa := fmt.Sprintf("-c:a aac -ar %d -b:a %d -ac %d ", av.Audio.Samplerate, av.Audio.Bitrate, av.Audio.Channels)
	om := fmt.Sprintf("-t %f -max_interleave_delta 0 -flush_packets 0 -copyts -muxpreload 0 -muxdelay 0 -f mpegts -", maxdur.Seconds())
	a, v := av.Audio.On(), av.Video.On()
	if v {
		ff += iv
	}
	if a {
		ff += ia
	}
	if v {
		ff += ov
	}
	if a {
		ff += oa
	}
	ff += om
	println(ff)
	s := strings.Split(ff, " ")
	cmd := exec.Command(s[0], s[1:]...)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

func makeBlackout(av *Info, maxdur time.Duration) ([]byte, error) {
	if maxdur == 0 {
		maxdur, _ = time.ParseDuration(fmt.Sprintf("%ss", av.Dur))
	}
	if maxdur == 0 {
		maxdur = 12 * time.Second
	}
	maxdur = 12 * time.Second
	ff := "ffmpeg -hide_banner -v quiet "
	iv := fmt.Sprintf("-f lavfi -i color=black:s=%dx%d:r=%f ", av.Video.Width, av.Video.Height, av.Video.FPS)
	ia := "-f lavfi -i anullsrc "
	ov := fmt.Sprintf("-pix_fmt yuv420p -r %f -c:v %s -video_track_timescale 12800 -g 1 -forced-idr 1 -x264opts scenecut=0:stitchable=1:repeat-headers=1:nal-hrd=cbr -profile %s -level %s -b:v %d ",
		av.Video.FPS, av.Video.Codec, av.Video.Profile, av.Video.Level, av.Video.Bitrate.BPS)

	oa := fmt.Sprintf("-c:a aac -ar %d -b:a %d -ac %d ", av.Audio.Samplerate, av.Audio.Bitrate, av.Audio.Channels)
	om := fmt.Sprintf("-t %f -f mp4 -min_frag_duration 20000000 -movflags empty_moov+default_base_moof+skip_trailer -", maxdur.Seconds())
	a, v := av.Audio.On(), av.Video.On()
	if v {
		ff += iv
	}
	if a {
		ff += ia
	}
	if v {
		ff += ov
	}
	if a {
		ff += oa
	}
	ff += om
	println(ff)
	s := strings.Split(ff, " ")
	cmd := exec.Command(s[0], s[1:]...)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}
