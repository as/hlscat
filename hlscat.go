/*
go mod init
go get github.com/as/hls@v0.3.3
go run hlscat.go > segment.ts
vlc segment.ts

go run hlscat.go http://example.com/manifest.m3u8 > segment.ts
vlc segment.ts

-muxpreload 0 -muxdelay 0
*/
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/as/hls"
)

const Blocksize = 16

var (
	verbose  = flag.Bool("v", false, "verbose output")
	noads    = flag.Bool("noads", false, "trim away all ad breaks")
	maxhttp  = flag.Int("maxhttp", 128, "max http conns")
	maxbuf   = flag.Int("maxbuf", 128*1024, "max buffer size")
	skip     = flag.Int("skip", 0, "debugging: skip this amount of segments (after any filters are applied)")
	count    = flag.Int("count", 0, "debugging: limit the number of segments processed")
	debug    = flag.Int("debug", 0, "debug level")
	noinit   = flag.Bool("noinit", false, "skip init atoms")
	nodec    = flag.Bool("nodec", false, "never decrypt anything")
	nofilter = flag.Bool("nofilter", false, "never fix ts segments")
	cbcbuf   = flag.Int("cbcbuf", 4096+16, "cbc buffer size")

	recurse    = flag.Bool("r", false, "recurse into media manifests if target is a master")
	ls         = flag.Bool("ls", false, "list segments found in manifests")
	ls2        = flag.Bool("l", false, "alias for ls")
	abs        = flag.Bool("abs", false, "force absolute paths when listing")
	print      = flag.Bool("print", false, "print the manifest to stderr after applying all transformations")
	selectexpr = flag.String("t", "", "select time range expression (s+e) or (s-e)")

	blackout      = flag.Bool("blackout", false, "blackout any ad content (not working)")
	blackoutdebug = flag.Bool("blackoutdebug", false, "blackoutdebug")

	base string

	proto = Info{}
)

var sem = make(chan bool, *maxhttp)

func init() {
	for i := 0; i < cap(sem); i++ {
		sem <- true
	}
}

func init() {
	var nothing bool
	flag.BoolVar(&nothing, "z", false, "z flags serve as a prototype for media manifests without a master; they describe codec settings")
	flag.IntVar(&proto.Video.Height, "z.h", 540, "video width")
	flag.IntVar(&proto.Video.Width, "z.w", 960, "video height")
	flag.Float64Var(&proto.Video.FPS, "z.fps", 25, "frame rate")
	flag.StringVar(&proto.Video.Codec, "z.vcodec", "h264", "video codec")
	flag.StringVar(&proto.Video.Profile, "z.profile", "high", "video profile")
	flag.StringVar(&proto.Video.Level, "z.level", "4", "video level")
	flag.IntVar(&proto.Video.Bitrate.BPS, "z.vbps", 1000000, "video bitrate")
	flag.IntVar(&proto.Audio.Bitrate, "z.abps", 192000, "audio bitrate")
	flag.IntVar(&proto.Audio.Samplerate, "z.arate", 48000, "audio sample rate")
	flag.IntVar(&proto.Audio.Channels, "z.channels", 2, "audio channel count")
}

func main() {
	http.DefaultClient.Transport = &http.Transport{
		ReadBufferSize:      8192,
		DisableCompression:  true,
		DisableKeepAlives:   false,
		MaxIdleConnsPerHost: 25,
		ForceAttemptHTTP2:   true,
	}
	flag.Parse()

	if *cbcbuf%16 != 0 {
		*cbcbuf = 4096 + 16 // clearly the user is unwell
	}
	if *ls2 {
		*ls = true
	}
	a := flag.Args()
	var src io.Reader
	if len(a) > 0 {
		if strings.HasPrefix(a[0], "http") {
			src = strings.NewReader(string(download(a[0])))
			if n := strings.LastIndex(a[0], "/"); n > 0 {
				base = a[0][:n] + "/"
				println("base path is", base)
			}
		} else {
			d, _ := ioutil.ReadFile(a[0])
			src = strings.NewReader(string(d))
		}
	} else {
		src = os.Stdin
	}

	tags, multi, err := hls.Decode(src)
	if err != nil {
		panic(err)
	}
	if !multi {
		m := &hls.Media{URL: a[0]}
		err = m.DecodeTag(tags...)
		if err != nil {
			panic(err)
		}
		if len(a) == 1 {
			io.Copy(os.Stdout, media(m))
			os.Exit(0)
		} else {
			s0 := media(m)
			m := &hls.Media{URL: a[1]}
			err = m.Decode(strings.NewReader(string(download(a[1]))))
			if err != nil {
				panic(err)
			}
			s1 := media(m)
			println("video", a[0], "audio", a[1])
			io.Copy(os.Stdout, filterMerge(s0, s1))
			os.Exit(0)
		}
	}
	m := &hls.Master{URL: a[0]}
	err = m.DecodeTag(tags...)
	if err != nil {
		panic(err)
	}
	if *ls {
		master(m)
		os.Exit(0)
	}
	mv, ma := selectBest(m)
	fmt.Fprintf(os.Stderr, "hls video: %s\n", mv.Path(m.Path("")))
	fmt.Fprintf(os.Stderr, "hls audio: %s\n", ma.Path(m.Path("")))
	if mv != ma {
		fmt.Fprintf(os.Stderr, "hls multi-file a/v\n")
		io.Copy(os.Stdout, filterMerge(media(mv), media(ma)))
	} else {
		fmt.Fprintf(os.Stderr, "hls single-file a/v\n")
		io.Copy(os.Stdout, media(mv))
	}
}

func mediaPlaylist(uri string) *hls.Media {
	println(uri)
	m := &hls.Media{URL: uri}
	err := m.Decode(strings.NewReader(string(download(uri))))
	if err != nil {
		panic(err)
	}
	return m
}

func master(m *hls.Master) {
	listmaster(m, os.Stdout)
	if *print {
		m.Encode(os.Stdout)
	}
}

type Pathy interface {
	Path(string) string
}

func media(m *hls.Media) io.ReadCloser {
	fmt.Fprintf(os.Stderr, "media playlist duration=%s\n", hls.Runtime(m.File...))

	if *noads {
		m.File = filterAD(m.File...)
	}
	if *skip > 0 {
		if *skip > len(m.File) {
			m.File = m.File[:0]
		} else {
			m.File = m.File[*skip:]
		}
	}
	if *count > 0 {
		if *count < len(m.File) {
			m.File = m.File[:*count]
		}
	}
	if *selectexpr != "" {
		ts, te := parseSelectExpr(*selectexpr)
		p, q := selectrange(ts, te, m)
		if *debug > 5 {
			fmt.Fprintf(os.Stderr, "select range t(%d,%d) -> s(%d,%d)\n", ts.Unix(), te.Unix(), p, q)
		}
		m.File = m.File[p:q]
	}

	dst := &bytes.Buffer{}
	if *ls {
		if *abs {
			list(m, dst)
		} else {
			stat(m, dst)
		}
	} else {
		return cat(&proto, m)
	}
	if *print {
		m.Encode(os.Stderr)
	}
	return io.NopCloser(dst)
}

func cat(info *Info, m *hls.Media) (rc io.ReadCloser) {
	var blackstream []byte
	var err error
	if *blackout {
		blackstream, err = makeBlackout(info, m.Target)
		if err != nil {
			panic(err)
		}
		if *blackoutdebug {
			os.Stdout.Write(blackstream)
			os.Exit(0)
		}
	}
	outc := make(chan io.ReadCloser, *maxhttp)
	go func() {
		defer close(outc)
		keyfile := ""
		key, iv := "", ""
		init := ""
		masterurl := m.Path("")
		for i := range m.File {
			f := &m.File[i]
			if f.Key.Method == "NONE" {
				keyfile, key, iv = "", "", ""
			}
			if k := f.Key.URI; k != "" && k != keyfile {
				// download the key but only if its unique
				keyfile = k
				key = string(download(f.Key.Path(masterurl)))
			}
			iv = f.Key.IV
			blackout := *blackout && (f.IsAD() || (i+1)%2 == 0)
			if newinit := f.Map.Path(masterurl); newinit != "" && newinit != init && !*noinit && !blackout {
				fmt.Fprintf(os.Stderr, "streaming init segment: %s\n", newinit)
				if key == "" {
					outc <- stream(newinit)
				} else {
					outc <- decrypt(key, iv, stream(newinit))
				}
				init = newinit
			}
			if blackout {
				init = ""
				outc <- filterFrag(io.NopCloser(bytes.NewReader(blackstream)), f.Duration(0))
			} else if key != "" {
				if *debug > 1 {
					fmt.Fprintf(os.Stderr, "keyfil=%q key=%x iv=%q iv=%x\n", f.Key.Path(masterurl), key, iv, unhex(iv))
				}
				outc <- decrypt(key, iv, stream(f.Path(masterurl)))
			} else {
				outc <- stream(f.Path(masterurl))
			}
		}
	}()

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for out := range outc {
			_, err := io.Copy(pw, out)
			if err != nil {
				//			panic(err)
			}
			out.Close()
		}
	}()
	return filterFrag(pr, 0)
}

func location(u string) string {
	if !strings.HasPrefix(u, "http") {
		u = base + u
	}
	return u
}

func printlocation(u string) string {
	if *abs {
		return location(u)
	}
	return u
}

func download(u string) []byte {
	resp, err := http.Get(location(u))
	if err != nil {
		panic(err)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	return data
}

func stream(u string) io.ReadCloser {
	<-sem
	u = location(u)
	if *debug > 0 {
		println("start stream", u)
	}
	resp, err := http.Get(u)
	if err != nil {
		panic(err)
	}
	pr, pw := io.Pipe()
	go func() {
		bw := bufio.NewWriterSize(pw, *maxbuf)
		io.Copy(bw, resp.Body)
		sem <- true
		if *debug > 0 {
			println("fin stream", u)
		}
		resp.Body.Close()
		bw.Flush()
		pw.Close()
	}()
	br := bufio.NewReaderSize(pr, *maxbuf)
	return io.NopCloser(br)
}

func js(v any) string {
	d, _ := json.Marshal(v)
	return string(d)
}
