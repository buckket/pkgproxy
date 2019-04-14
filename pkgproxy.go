/*
pkgproxy is a caching proxy server specifically designed for caching Arch GNU/Linux packages for pacman.

Usage:
  pkgproxy [options]

  Options:
    -cache string
        Cache base path (default: $XDG_CACHE_HOME)
    -port string
        Listen on addr (default ":8080")
    -upstream string
        Upstream URL (default "https://mirrors.kernel.org/archlinux/$repo/os/$arch")
    -version bool
        Show version information
*/
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

const version = "1.0.0"

var CacheMap = make(map[string]string)
var MutexMap = make(map[string]*sync.Mutex)

type Request struct {
	Repo string
	OS   string
	Arch string
	File string
}

type Settings struct {
	CacheDir       string
	UpstreamServer string
}

var GSettings Settings

func setupCacheDir() {
	err := os.RemoveAll(GSettings.CacheDir)
	if err != nil {
		panic(err)
	}
	err = os.Mkdir(GSettings.CacheDir, 0700)
	if err != nil {
		panic(err)
	}
}

func destroyCacheDir() {
	err := os.RemoveAll(GSettings.CacheDir)
	if err != nil {
		panic(err)
	}
}

func renameTempFile(filename *string) error {
	return os.Rename(path.Join(GSettings.CacheDir, "."+*filename), path.Join(GSettings.CacheDir, *filename))
}

func removeTempFile(filename *string) error {
	return os.Remove(path.Join(GSettings.CacheDir, "."+*filename))
}

func buildUpstreamURL(req *Request) string {
	upstreamURL := strings.Replace(GSettings.UpstreamServer, "$repo", req.Repo, 1)
	upstreamURL = strings.Replace(upstreamURL, "$arch", req.Arch, 1)
	return upstreamURL + "/" + req.File
}

func splitReqURL(requestURL string) (Request, error) {
	URLSplit := strings.Split(requestURL, "/")[1:]
	if len(URLSplit) < 4 || len(URLSplit[3]) < 3 {
		return Request{}, errors.New("invalid URL")
	}
	return Request{URLSplit[0], URLSplit[1], URLSplit[2], URLSplit[3]}, nil
}

func buildCacheKey(reqURL *string, resp *http.Response) string {
	u, err := url.Parse(*reqURL)
	if err != nil {
		return ""
	}
	cacheKey := fmt.Sprintf("%s::", u.Hostname())
	if len(resp.Header.Get("ETag")) > 0 {
		cacheKey += strings.Trim(resp.Header.Get("ETag"), "\"")
	} else if len(resp.Header.Get("Last-Modified")) > 0 {
		cacheKey += resp.Header.Get("Last-Modified")
	} else {
		cacheKey = ""
	}
	return cacheKey
}

func handleRequest(w http.ResponseWriter, r *http.Request, req *Request) {
	var isCached, isDB bool
	var fileError, respError bool
	var resp *http.Response
	var file *os.File
	var err error
	var cacheKey string

	reqURL := buildUpstreamURL(req)

	_, ok := MutexMap[req.File]
	if !ok {
		MutexMap[req.File] = &sync.Mutex{}
	}
	MutexMap[req.File].Lock()
	defer delete(MutexMap, req.File)

	if strings.HasSuffix(req.File, ".db") {
		isDB = true
		resp, err = http.Head(reqURL)
		if err != nil {
			log.Printf("(%s)[Upstream] Failed to query host, sending %q", req.File, http.StatusText(http.StatusInternalServerError))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else if resp.StatusCode != http.StatusOK {
			defer resp.Body.Close()
			log.Printf("(%s)[Upstream] Host responded with %d (%s)", req.File, resp.StatusCode, http.StatusText(resp.StatusCode))
			http.Error(w, http.StatusText(resp.StatusCode), resp.StatusCode)
			return
		}
		defer resp.Body.Close()
		cacheKey = buildCacheKey(&reqURL, resp)
	}

	if !isDB || (isDB && CacheMap[req.Repo] == cacheKey) {
		file, err = os.Open(path.Join(GSettings.CacheDir, req.File))
		if err != nil {
			file, err = os.Create(path.Join(GSettings.CacheDir, "."+req.File))
			if err != nil {
			} else {
				defer file.Close()
			}
		} else {
			defer file.Close()
			isCached = true
		}
	} else {
		log.Printf("(%s)[Local] Cached version is outdated, requesting new file", req.File)
		file, err = os.Create(path.Join(GSettings.CacheDir, "."+req.File))
		if err != nil {
		} else {
			defer file.Close()
		}
	}

	if isCached {
		log.Printf("(%s)[Meta] Serving cached version", req.File)
		w.Header().Set("Content-Type", "application/octet-stream")
		lastmod := time.Time{}
		if isDB {
			w.Header().Set("Content-Length", resp.Header.Get("Content-Length"))
			w.Header().Set("Last-Modified", resp.Header.Get("Last-Modified"))
			w.Header().Set("ETag", resp.Header.Get("ETag"))
			lastmod, _ = time.Parse(http.TimeFormat, resp.Header.Get("Last-Modified"))
		}
		http.ServeContent(w, r, req.File, lastmod, file)
	} else {
		log.Printf("(%s)[Meta] Forwarding and saving to cache", req.File)
		resp, err := http.Get(reqURL)
		if err != nil {
			file.Close()
			removeTempFile(&req.File)
			log.Printf("(%s)[Upstream] Failed to query host, sending %q", req.File, http.StatusText(http.StatusInternalServerError))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else if resp.StatusCode != http.StatusOK {
			defer resp.Body.Close()
			file.Close()
			removeTempFile(&req.File)
			log.Printf("(%s)[Upstream] Host responded with %d (%s)", req.File, resp.StatusCode, http.StatusText(resp.StatusCode))
			http.Error(w, http.StatusText(resp.StatusCode), resp.StatusCode)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Length", resp.Header.Get("Content-Length"))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Last-Modified", resp.Header.Get("Last-Modified"))
		w.Header().Set("ETag", resp.Header.Get("ETag"))
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if err != nil && err != io.EOF {
				panic(err)
			}
			if n == 0 || (fileError && respError) {
				break
			}
			if !fileError {
				if _, err := file.Write(buf[:n]); err != nil {
					log.Printf("(%s)[Local] %s", req.File, err)
					fileError = true
				}
			}
			if !respError {
				if _, err := w.Write(buf[:n]); err != nil {
					log.Printf("(%s)[Forward] %s", req.File, err)
					respError = true
				}
			}
		}

		if !fileError {
			err = renameTempFile(&req.File)
			if err != nil {
				log.Printf("(%s)[Local] Could not rename temp file", req.File)
			} else {
				log.Printf("(%s)[Local] Successfully cached", req.File)
			}
			if isDB {
				CacheMap[req.Repo] = cacheKey
			}
		} else {
			file.Close()
			removeTempFile(&req.File)
			log.Printf("(%s)[Local] Could not cache", req.File)
		}
		if !respError {
			log.Printf("(%s)[Forward] Successfully forwarded", req.File)
		} else {
			log.Printf("(%s)[Forward] Error while forwarding", req.File)
		}
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Printf("[Incoming] Request for URL: %s\n", r.URL)

	if r.Method != "GET" {
		log.Printf("[Incoming] We don't do %q, sending %q", r.Method, http.StatusText(http.StatusNotImplemented))
		http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
		return
	}

	req, err := splitReqURL(r.URL.String())
	if err != nil {
		log.Printf("[Incoming] URL invalid, sending %q", http.StatusText(http.StatusBadRequest))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	handleRequest(w, r, &req)
}

func main() {
	flCachePath := flag.String("cache", "", "Cache base path")
	flAddr := flag.String("port", ":8080", "Listen on addr")
	flUpstream := flag.String("upstream", "https://mirrors.kernel.org/archlinux/$repo/os/$arch", "Upstream URL")
	flShowVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *flShowVersion {
		fmt.Printf("pkgproxy %s\n", version)
		return
	}

	if len(*flCachePath) > 0 {
		GSettings.CacheDir = *flCachePath
	} else {
		var err error
		GSettings.CacheDir, err = os.UserCacheDir()
		if err != nil {
			panic(err)
		}
	}
	GSettings.CacheDir = path.Join(GSettings.CacheDir, "pkgproxy")
	GSettings.UpstreamServer = *flUpstream

	setupCacheDir()
	defer destroyCacheDir()

	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(*flAddr, nil))
}
