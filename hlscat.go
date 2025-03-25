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
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

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

	recurse = flag.Bool("r", false, "recurse into media manifests if target is a master")
	ls      = flag.Bool("ls", false, "list segments found in manifests")
	ls2     = flag.Bool("l", false, "alias for ls")
	abs     = flag.Bool("abs", false, "force absolute paths when listing")
	print   = flag.Bool("print", false, "print the manifest to stderr after applying all transformations")

	blackout = flag.Bool("blackout", false, "blackout any ad content (not working)")
	blackoutdebug = flag.Bool("blackoutdebug", false, "blackoutdebug")

	base string

	proto = Info{}
)

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

func main() {
	http.DefaultClient.Transport = &http.Transport{
		ReadBufferSize:      8192,
		DisableCompression:  true,
		DisableKeepAlives:   false,
		MaxIdleConnsPerHost: 25,
		ForceAttemptHTTP2:   true,
	}
	flag.Parse()
	// p,err:=makeBlackout(*mba, *mbv, 10)

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
		media(m, os.Stdout)
		os.Exit(0)
	}
	m := &hls.Master{URL: a[0]}
	err = m.DecodeTag(tags...)
	if err != nil {
		panic(err)
	}
	println("master")
	master(m)

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

func listmaster(m *hls.Master, dst io.Writer) (err error) {
	link := m.Path("")
	dolist := func(f Pathy) {
		link := f.Path(link)
		if *abs {
			fmt.Fprintf(dst, "%s\n", link)
		} else {
			fmt.Fprintf(dst, "%s\n", f.Path(""))
		}
		if *recurse {
			m := &hls.Media{URL: link}
			err := m.Decode(strings.NewReader(string(download(link))))
			if err != nil {
				panic(err)
			}
			media(m, dst)
			fmt.Fprintln(dst)
			fmt.Fprintln(dst)
		}
	}
	for i := range m.Media {
		dolist(&m.Media[i])
	}
	for i := range m.Stream {
		dolist(&m.Stream[i])
	}
	for i := range m.IFrame {
		dolist(&m.IFrame[i])
	}
	return nil
}

func media(m *hls.Media, dst io.Writer) {
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

	if *ls {
		if *abs {
			list(m, dst)
		} else {
			stat(m, dst)
		}
	} else {
		cat(&proto, m, dst)
	}
	if *print {
		m.Encode(os.Stderr)
	}
}

func list(m *hls.Media, dst io.Writer) (err error) {
	init := ""
	for i := range m.File {
		f := &m.File[i]
		if newinit := f.Map.URI; newinit != "" && newinit != init && !*noinit {
			fmt.Fprintf(dst, "%s\n", printlocation(f.Map.URI))
			init = newinit
		}
		fmt.Fprintf(dst, "%s\n", printlocation(f.Inf.URL))
	}
	return nil
}

func stat(m *hls.Media, dst io.Writer) (err error) {
	init := ""
	t := time.Duration(0)
	for i := range m.File {
		f := &m.File[i]
		if newinit := f.Map.URI; newinit != "" && newinit != init && !*noinit {
			fmt.Fprintf(dst, "%s\n", printlocation(f.Map.URI))
			init = newinit
		}
		d := f.Duration(0)
		t += d
		fmt.Fprintf(dst, "%s	d=%f	t=%f\n", printlocation(f.Inf.URL), d.Seconds(), t.Seconds())
	}
	return nil
}

func cat(info *Info, m *hls.Media, dst io.Writer) (err error) {
	defer func() {
		if err != nil {
			return
		}
		//err, _ = recover().(error)
	}()
	var blackstream []byte
	if *blackout {
		blackstream, err = makeBlackout(info, m.Target)
		if err != nil {
			panic(err)
		}
		if *blackoutdebug{
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
		for i := range m.File {
			f := &m.File[i]
			if k := f.Key.URI; k != "" && k != keyfile {
				// download the key but only if its unique
				keyfile = k
				key = string(download(f.Key.Path(m.Path(""))))
			}
			iv = f.Key.IV
			blackout := *blackout && (f.IsAD()  || (i+1)%2==0)
			if newinit := f.Map.Path(m.Path("")); newinit != "" && newinit != init && !*noinit && !blackout{
				fmt.Fprintf(os.Stderr, "streaming init segment: %s\n", newinit)
				if key == "" {
					outc <- stream(newinit)
				} else {
					outc <- decrypt(key, iv, stream(newinit))
				}
				init = newinit
			}
			if blackout{
				init = ""
				outc <- filterFrag(io.NopCloser(bytes.NewReader(blackstream)), f.Duration(0))
			} else if key != "" {
				if *debug > 1 {
					fmt.Fprintf(os.Stderr, "keyfil=%q key=%x iv=%q iv=%x\n", f.Key.Path(m.Path("")), key, iv, unhex(iv))
				}
				outc <- decrypt(key, iv, stream(f.Inf.URL))
			} else {
				outc <- stream(f.Inf.URL)
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
	//_, err = io.Copy(dst, filterTS(pr))
	_, err = io.Copy(dst, filterFrag(pr, 0))
	if err != nil{
		panic(err)
	}
	//	io.Copy(dst, filterTS(pr))
	return
}

func filterTS(r io.ReadCloser, trim time.Duration) io.ReadCloser {
	if *nofilter {
		return r
	}
	cmdline := "ffmpeg -hide_banner -thread_queue_size 2048 -dts_delta_threshold 1 -i - -c copy -fflags +shortest+genpts -muxpreload 0 -muxdelay 0 -max_interleave_delta 0 -flush_packets 0 -f mpegts -"
	if trim != 0 {
		cmdline += fmt.Sprintf("t %f -", trim.Seconds())
	} 
	s := strings.Split(cmdline, " ")
	println(cmdline)
	fmt.Fprintf(os.Stderr, "%q\n", s)
	cmd := exec.Command(s[0], s[1:]...)
	cmd.Stdin = r
	if *debug > 3 {
		cmd.Stderr = os.Stderr
	}
	w, _ := cmd.StdoutPipe()
	err := cmd.Start()
	if err != nil{
		panic(err)
	}
	go func() {
		cmd.Wait()
	}()
	return w
}

func bitty() string{
	return fmt.Sprintf("-bsf setts=pts=(2.02*12880)+N*512")
}

func filterFrag(r io.ReadCloser, trim time.Duration) io.ReadCloser {
	cmdline := "ffmpeg -hide_banner -thread_queue_size 2048 -i - -c copy -f mp4 -min_frag_duration 30000000 -movflags empty_moov+default_base_moof+skip_trailer -"
	if trim != 0 {
		cmdline += fmt.Sprintf("t %f %s -", trim.Seconds(), bitty())
	} 
	println(cmdline)
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

// decrypt decrypts the contents of the reader using the key and iv
// in aes128cbc mode. it automatically unpads the last block when
// the reader encounters an eof condition
func decrypt(key, iv string, r io.ReadCloser) io.ReadCloser {
	if *nodec || key == "" {
		return r
	}
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		r := bufio.NewReader(r)
		// the reason we make two buffers is because we need to delay
		// writes to the pipe by 16 bytes, since only the last block is padded
		// and we dont know when that is in a stream
		//
		// the buffers are swapped and output is delayed to detect this condition
		tmp0, tmp1 := make([]byte, *cbcbuf), make([]byte, *cbcbuf)
		//tmp0, tmp1 = make([]byte, 16), make([]byte, 16)
		block, err := aes.NewCipher([]byte(key))
		if err != nil {
			panic(err)
		}
		lastblock := func(msg []byte) {
			// this is the last block, so we must unpad it
			msg, err := unpad(msg)
			if err != nil {
				panic(err)
			}
			pw.Write(msg)
		}
		cbc := cipher.NewCBCDecrypter(block, unhex(iv))
		//n, err := io.ReadAtLeast(r, tmp0, Blocksize)
		n, err := readMod16(r, tmp0)
		for n >= 16 {
			msg := tmp0[:n]
			cbc.CryptBlocks(msg, msg)
			if err != nil {
				lastblock(msg) // its possible to get 16 bytes and an io.EOF
				break
			}

			//n, err = io.ReadAtLeast(r, tmp1, 16)
			n, err = readMod16(r, tmp1)
			if err == nil {
				pw.Write(msg)
				tmp0, tmp1 = tmp1, tmp0 // swap buffers
			} else {
				lastblock(msg) // but more commonly the eof is on the next write
			}
		}
		if err != nil && err != io.EOF {
			panic(err)
		}
	}()
	return pr
}

func readMod16(r io.Reader, p []byte) (n int, err error) {
	if len(p)%16 != 0 {
		panic("readMod16: p % 16 != 0 (developer error)")
	}
	n, err = io.ReadAtLeast(r, p[:len(p)-16], 16)
	if need := 16 - (n % 16); need != 0 && need != 16 {
		m := 0
		m, err = io.ReadFull(r, p[n:n+need])
		n += m
	}
	return
}

// unpad applies the PKCS7 algorithm to unpad a plaintext CBC message 'm'
// m should be exactly 16 bytes. As per the standard, there will always be at
// least ONE padding byte, because a padding length of zero indicates that
// all 16 bytes are padding.
//
// This function should be called in a system that applies the standard PKCS7
// padding on the final block of the message, and it should be called AFTER
// decryption.
func unpad(m []byte) (q []byte, err error) {
	if len(m) < Blocksize {
		panic("block too small")
	}
	padlen := m[len(m)-1]
	if padlen > Blocksize {
		return nil, fmt.Errorf("bad pad length: %d > %d", padlen, Blocksize)
	}
	if padlen == 0 {
		// if the pad length is 0, the entire block is padded and will be thrown away
		// some non compliant implementations zero means "no padding", which
		// doesn't make any sense because that zero itself is one byte of padding
		padlen = Blocksize
	}

	// check the value of the actual padding, each byte needs to be padlen
	// this detects ciphertext tampering and acts as a CRC
	psi := len(m) - int(padlen) // pad start index
	for _, v := range m[psi:] {
		if v != padlen {
			return nil, fmt.Errorf("bad pad value: %d != %d", v, padlen)
		}
	}
	return m[:psi], nil
}

func unhex(a string) (h []byte) {
	h = []byte(strings.TrimPrefix(a, "0x"))
	hexlen, err := hex.Decode(h, h)
	if err != nil {
		panic(a)
	}
	return h[:hexlen]
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

var sem = make(chan bool, *maxhttp)

func init() {
	for i := 0; i < cap(sem); i++ {
		sem <- true
	}
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
