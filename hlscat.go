/*
go mod init
go get github.com/as/hls@v0.3.3
go run hlscat.go > segment.ts
vlc segment.ts

go run hlscat.go http://example.com/manifest.m3u8 > segment.ts
vlc segment.ts
*/
package main

import (
	"bufio"
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

	recurse = flag.Bool("r", false, "recurse into media manifests if target is a master")
	ls      = flag.Bool("ls", false, "list segments found in manifests")
	ls2     = flag.Bool("l", false, "alias for ls")
	abs     = flag.Bool("abs", false, "force absolute paths when listing")
	print   = flag.Bool("print", false, "print the manifest to stderr after applying all transformations")

	base string
)

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
	flag.Parse()
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
		stat(m, dst)
	} else {
		cat(m, dst)
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

func cat(m *hls.Media, dst io.Writer) (err error) {
	defer func() {
		if err != nil {
			return
		}
		err, _ = recover().(error)
	}()
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
			if newinit := f.Map.Path(m.Path("")); newinit != "" && newinit != init && !*noinit {
				fmt.Fprintf(os.Stderr, "streaming init segment: %s\n", newinit)
				if key == "" {
					outc <- stream(newinit)
				} else {
					outc <- decrypt(key, iv, stream(newinit))
				}
				init = newinit
			}
			if key != "" {
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
				panic(err)
			}
			out.Close()
		}
	}()
	io.Copy(dst, filterTS(io.NopCloser(bufio.NewReaderSize(pr, *maxbuf))))
	//	io.Copy(dst, filterTS(pr))
	return
}

func filterTS(r io.ReadCloser) io.ReadCloser {
	if *nofilter {
		return r
	}
	s := strings.Split("ffmpeg -hide_banner -thread_queue_size 2048 -i - -c copy -fflags +shortest+genpts -max_interleave_delta 0 -flush_packets 0 -f mpegts -", " ")
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
		tmp0, tmp1 := make([]byte, Blocksize), make([]byte, Blocksize)
		block, err := aes.NewCipher([]byte(key))
		if err != nil {
			panic(err)
		}
		lastblock := func(msg []byte) {
			// this is the last block, so we must unpad it
			msg, err := unpad(msg)
			if err != nil {
				println(err)
			}
			pw.Write(msg)
		}
		cbc := cipher.NewCBCDecrypter(block, unhex(iv))
		n, err := io.ReadAtLeast(r, tmp0, Blocksize)
		for n >= 16 {
			msg := tmp0[:n]
			cbc.CryptBlocks(msg, msg)
			if err != nil {
				lastblock(msg) // its possible to get 16 bytes and an io.EOF
				break
			}

			n, err = io.ReadAtLeast(r, tmp1, 16)
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
