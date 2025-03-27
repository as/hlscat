package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/as/hls"
)

func bitty() string {
	return fmt.Sprintf("-bsf setts=pts=(2.02*12880)+N*512")
}

func filterAD(file ...hls.File) (new []hls.File) {
	for _, f := range file {
		if f.IsAD() {
			fmt.Fprintf(os.Stderr, "skipping ad break: %s\n", f.Inf.URL)
			continue
		}
		new = append(new, f)
	}
	return
}

func filterFrag(r io.ReadCloser, trim time.Duration) io.ReadCloser {
	cmdline := "ffmpeg -hide_banner -thread_queue_size 4096 -i - -c copy -f mp4 -min_frag_duration 10000000 -bsf:a aac_adtstoasc -movflags empty_moov+default_base_moof+skip_trailer -"
	if trim != 0 {
		cmdline += fmt.Sprintf("t %f %s -", trim.Seconds(), bitty())
	}
	println("filterFrag", cmdline)
	s := strings.Split(cmdline, " ")
	fmt.Fprintf(os.Stderr, "%q\n", s)
	cmd := exec.Command(s[0], s[1:]...)
	cmd.Stdin = r
	if *debug > 3 {
		cmd.Stderr = os.Stderr
	}
	w, _ := cmd.StdoutPipe()
	go func() {
		cmd.Start()
		cmd.Wait()
	}()
	return w
}

func filterTS(r io.ReadCloser, trim time.Duration) io.ReadCloser {
	if *nofilter {
		return r
	}
	cmdline := "ffmpeg -hide_banner -thread_queue_size 4096 -dts_delta_threshold 1 -i - -c copy -fflags +shortest+genpts -muxpreload 0 -muxdelay 0 -max_interleave_delta 0 -flush_packets 0 -f mpegts -"
	if trim != 0 {
		cmdline += fmt.Sprintf("t %f -", trim.Seconds())
	}
	s := strings.Split(cmdline, " ")
	println("filterTS", cmdline)
	fmt.Fprintf(os.Stderr, "%q\n", s)
	cmd := exec.Command(s[0], s[1:]...)
	cmd.Stdin = r
	if *debug > 3 {
		cmd.Stderr = os.Stderr
	}
	w, _ := cmd.StdoutPipe()
	err := cmd.Start()
	if err != nil {
		panic(err)
	}
	go func() {
		cmd.Wait()
	}()
	return w
}

func filterMerge(s0, s1 io.ReadCloser) io.ReadCloser {
	cmdline := "ffmpeg -hide_banner -thread_queue_size 4096 -i - -thread_queue_size 4096 -i /proc/self/fd/3 -c copy -map 0:v -map 1:a -bsf:a aac_adtstoasc -f mp4 -min_frag_duration 10000000 -movflags empty_moov+default_base_moof+skip_trailer -"
	println("filterMerge", cmdline)
	s := strings.Split(cmdline, " ")
	fmt.Fprintf(os.Stderr, "%q\n", s)
	cmd := exec.Command(s[0], s[1:]...)
	cmd.Stdin = s0
	pr, pw, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	done := make(chan bool)
	go func() {
		io.Copy(pw, s1)
		pw.Close()
		close(done)
	}()
	cmd.ExtraFiles = append(cmd.ExtraFiles, pr)
	if *debug > 3 {
		cmd.Stderr = os.Stderr
	}
	w, _ := cmd.StdoutPipe()
	go func() {
		cmd.Start()
		<-done
		cmd.Wait()
	}()
	return w
}
