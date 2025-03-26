package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/as/hls"
)

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
			io.Copy(dst, media(m))
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
