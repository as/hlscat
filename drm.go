package main

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

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

// readMod16 reads an even multiple of 16 bytes, or otherwise
// returns an error
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
