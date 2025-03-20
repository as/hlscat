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
	"strings"

	"github.com/as/hls"
)

const Blocksize = 16

var (
	verbose = flag.Bool("v", false, "verbose output")
	noads   = flag.Bool("noads", false, "trim away all ad breaks")
	base    string
)

func main() {
	flag.Parse()
	a := flag.Args()
	var src io.Reader
	if len(a) > 0 {
		println("downloading from", a[0])
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
		println("using stdin for manifest")
		src = os.Stdin
	}

	m := hls.Media{}
	err := m.Decode(src)
	if err != nil {
		panic(err)
	}
	fmt.Fprintf(os.Stderr, "media playlist duration=%s\n", hls.Runtime(m.File...))

	keyfile := ""
	key, iv := "", ""
	init := ""
	for i := range m.File {
		f := &m.File[i]
		if *noads && f.IsAD() {
			fmt.Fprintf(os.Stderr, "skipping ad break: %s\n", f.Inf.URL)
			continue
		}
		if k := f.Key.URI; k != "" && k != keyfile {
			// download the key but only if its unique
			keyfile = k
			key = string(download(k))
		}
		iv = f.Key.IV
		fmt.Fprintf(os.Stderr, "key=%x iv=%q iv=%x\n", key, iv, unhex(iv))
		if newinit := f.Init(nil).String(); newinit != "" && newinit != init {
			fmt.Fprintf(os.Stderr, "streaming init segment: %s", newinit)
			io.Copy(os.Stdout, stream(newinit))
			init = newinit
		}
		io.Copy(os.Stdout, decrypt(key, iv, stream(f.Inf.URL)))
	}
	m.Encode(os.Stderr)
}

// decrypt decrypts the contents of the reader using the key and iv
// in aes128cbc mode. it automatically unpads the last block when
// the reader encounters an eof condition
func decrypt(key, iv string, r io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		if key == "" {
			println("nothing to decrypt, streaming as plaintext")
			io.Copy(pw, r)
			return
		}
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
			pw.Write(msg)
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
func unpad(m []byte) ([]byte, error) {
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

func download(u string) []byte {
	if !strings.HasPrefix(u, "http") {
		u = base + u
	}
	resp, err := http.Get(u)
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
	if !strings.HasPrefix(u, "http") {
		u = base + u
	}
	resp, err := http.Get(u)
	if err != nil {
		panic(err)
	}
	return resp.Body
}

func js(v any) string {
	d, _ := json.Marshal(v)
	return string(d)
}
