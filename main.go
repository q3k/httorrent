package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/golang/glog"

	"github.com/anacrolix/torrent"
)

func init() {
	flag.Set("logtostderr", "true")
}

var (
	reHex = regexp.MustCompile("[a-fA-F0-9]+")

	flagListen string
)

func main() {
	flag.StringVar(&flagListen, "listen", "127.0.0.1:2137", "Address to listen on")
	flag.Parse()

	c, err := torrent.NewClient(nil)
	if err != nil {
		glog.Exitf("%v", err)
	}
	defer c.Close()

	c.AddDHTNodes([]string{
		"82.221.103.244:6881",
		"67.215.246.10:6881",
		"87.98.162.88:6881",
		"174.129.43.152:6881",
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		c.WriteStatus(w)
	})

	http.HandleFunc("/t/", func(w http.ResponseWriter, r *http.Request) {
		path := r.RequestURI[len("/t/"):]
		parts := strings.Split(path, "/")
		for i, part := range parts {
			parts[i], _ = url.QueryUnescape(part)
		}

		magnet := parts[0]

		if !strings.HasPrefix(magnet, "magnet:?xt=urn:btih:") {
			w.WriteHeader(404)
			fmt.Fprintf(w, "must be a magnet link\n")
			return
		}

		hash := magnet[20:]
		if !reHex.MatchString(hash) {
			w.WriteHeader(404)
			fmt.Fprintf(w, "invalid magnet")
		}

		t, err := c.AddMagnet(magnet)
		if err != nil {
			w.WriteHeader(500)
			fmt.Fprintf(w, "error: %v\n", err)
			return
		}

		// sanitize
		magnet = magnet[:60]

		t.AddTrackers([][]string{
			{"udp://tracker.opentrackr.org:1337/announce"},
			{"udp://exodus.desync.com:6969"},
			{"udp://tracker.coppersurfer.tk:80"},
			{"udp://glotorrents.pw:6969"},
			{"udp://explodie.org:6969"},
		})

		glog.Infof("Getting info for %q...", magnet)
		<-t.GotInfo()

		wantFile := ""
		if len(parts) > 0 {
			wantFile = strings.Join(parts[1:], "/")
		}

		availFiles := make(map[string]bool)
		for _, f := range t.Files() {
			availFiles[f.DisplayPath()] = true
		}

		glog.Infof("%v: available files: %+v", magnet, availFiles)

		if wantFile == "" {
			if len(availFiles) > 1 {
				w.Header().Add("Content", "text/html")
				w.WriteHeader(200)
				fmt.Fprintf(w, "available files:")
				fmt.Fprintf(w, "<ul>")
				for af, _ := range availFiles {
					fmt.Fprintf(w, "<a href=\"/t/%s/%s\">%s</a>", url.QueryEscape(magnet), quotePath(af), af)
				}
				fmt.Fprintf(w, "</ul>")
				return
			}
			// redirect to first (only) available file
			for af, _ := range availFiles {
				http.Redirect(w, r, fmt.Sprintf("/t/%s/%s", url.QueryEscape(magnet), quotePath(af)), 301)
				return
			}
		}

		glog.Infof("%v: want file %q", magnet, wantFile)

		var fi *torrent.File
		for _, f := range t.Files() {
			if f.DisplayPath() != wantFile {
				continue
			}

			fi = f
			break
		}
		if fi == nil {
			w.WriteHeader(404)
			fmt.Fprintf(w, "no such file")
			return
		}

		glog.Infof("%v: file: %+v", magnet, fi)

		reader := fi.NewReader()
		defer reader.Close()
		http.ServeContent(w, r, wantFile, time.Unix(0, 0), reader)
		glog.Infof("%v: done", magnet)
	})

	glog.Infof("Listening on %q", flagListen)
	http.ListenAndServe(flagListen, nil)
}

func quotePath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.QueryEscape(part)
	}
	return strings.Join(parts, "/")
}
