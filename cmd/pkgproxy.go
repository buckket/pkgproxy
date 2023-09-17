/*
pkgproxy is a caching proxy server specifically designed for caching Arch GNU/Linux packages for pacman.

Usage:

	pkgproxy [options]

	Options:
	  -cache string
	      Cache base path (default: $XDG_CACHE_HOME)
	  -keep-cache bool
	      Keep the cache between restarts
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
	"os"
	"path"
	"strings"
	"time"

	util "github.com/binary-manu/pkgproxy/cmd/internal"
)

/********************************************************************************
 * Type definitions
 ********************************************************************************/

type fileStatus int

// A CacheEntry is a reference-counted set of items shared among file downloads.
// File is used both for writing downloaded data (by one single goroutine) while
// other clients that want to also download the same file will read from File,
// while periodically checking if the size has changed due to appended data, As
// soon as the file has been fully downloaded, Complete is closed to signal
// other goroutines to quit. Started signals that the downloading gorountine has
// initialized file-specific fields and that reading can start.
type cacheEntry struct {
	RefCount uint
	Started  chan struct{}
	Complete chan struct{}
	File     util.WORMSeekCloser

	// The following fields should be accessed only after
	// Started has been closed
	HTTPInfo http.Header
}

type request struct {
	Repo       string
	OS         string
	Arch       string
	File       string
	CacheEntry *cacheEntry
	FileStatus fileStatus
}

type settings struct {
	CacheDir       string
	UpstreamServer string
}

type fileHandler = func(w http.ResponseWriter, r *http.Request, req *request)

type fileHandlerMap = map[fileStatus]fileHandler

/********************************************************************************
 * Constants
 ********************************************************************************/

var version = "HEAD"

const (
	_ fileStatus = iota
	// File has already been cached and can be served locally
	fileStatusCached
	// Another goroutine is already downloading this file
	fileStatusInDownload
	// The file is not in cache and no one is downloading it
	fileStatusMissing
	// The file will not be cached, but always redownloaded
	fileStatusNoCaching
)

/********************************************************************************
 * Globals
 ********************************************************************************/

var gSettings settings
var headersToForward = []string{"Content-Length", "Last-Modified", "ETag", "Content-Type"}
var sharedState util.Cache[string, *cacheEntry]
var fileHandlers = fileHandlerMap{
	fileStatusCached:     fileHandlerCached,
	fileStatusMissing:    fileHandlerMissingOrUncacheable,
	fileStatusNoCaching:  fileHandlerMissingOrUncacheable,
	fileStatusInDownload: fileHandlerInDownload,
}

/********************************************************************************
 * Methods: Request
 ********************************************************************************/

func newRequest(requestURL string) (request, error) {
	urlSplit := strings.Split(requestURL, "/")[1:]
	if len(urlSplit) < 4 || len(urlSplit[3]) < 3 {
		return request{}, errors.New("invalid URL")
	}
	return request{
		Repo: urlSplit[0], OS: urlSplit[1], Arch: urlSplit[2], File: urlSplit[3],
	}, nil
}

func (req *request) GetUpstreamURL() string {
	upstreamURL := strings.Replace(gSettings.UpstreamServer, "$repo", req.Repo, 1)
	upstreamURL = strings.Replace(upstreamURL, "$arch", req.Arch, 1)
	return upstreamURL + "/" + req.File
}

func (req *request) GetCachePathName() string {
	return path.Join(gSettings.CacheDir, req.File)
}

func (req *request) GetCacheTempPathName() string {
	return path.Join(gSettings.CacheDir, "."+req.File)
}

/********************************************************************************
 * Methods: CacheEntry
 ********************************************************************************/

// Create a new CacheEntry with a reference count of 1, an open Compete channel
// and File pointing to a (temporary) cache file where data should be written.
func newCacheEntryFromRequest(req *request) (*cacheEntry, error) {
	file, err := os.OpenFile(req.GetCacheTempPathName(), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}
	return &cacheEntry{
		RefCount: 1,
		Complete: make(chan struct{}),
		Started:  make(chan struct{}),
		File:     util.NewConcurrentWORMSeekCloser(file),
	}, nil
}

/********************************************************************************
 * Functions
 ********************************************************************************/

func setupCacheDir() error {
	err := os.MkdirAll(gSettings.CacheDir, 0777)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}

func destroyCacheDir() error {
	return os.RemoveAll(gSettings.CacheDir)
}

func renameTempFile(req *request, timestamp string) error {
	ts, tserr := time.Parse("Mon, 02 Jan 2006 15:04:05 GMT", timestamp)
	err := os.Rename(req.GetCacheTempPathName(), req.GetCachePathName())
	if tserr == nil {
		os.Chtimes(req.GetCachePathName(), time.Now(), ts)
	}
	return err
}

func fileHandlerCached(w http.ResponseWriter, r *http.Request, req *request) {
	log.Printf("(%s) Serving cached file", req.File)
	cachedData, err := os.Open(req.GetCachePathName())
	if err != nil {
		log.Printf("(%s) Failed to open cached file, sending %q, error %s", req.File, http.StatusText(http.StatusInternalServerError), err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer cachedData.Close()

	fileTime := time.Now()
	if fileStat, err := cachedData.Stat(); err == nil {
		fileTime = fileStat.ModTime()
	}
	http.ServeContent(w, r, req.File, fileTime, cachedData)
}

func fileHandlerMissingOrUncacheable(w http.ResponseWriter, _ *http.Request, req *request) {
	if req.FileStatus != fileStatusNoCaching {
		log.Printf("(%s) Forwarding and saving to cache", req.File)
	} else {
		log.Printf("(%s) Forwarding uncacheable file", req.File)
	}

	resp, err := http.Get(req.GetUpstreamURL())
	if err != nil {
		log.Printf("(%s) Failed to query host, sending %q, error: %s", req.File, http.StatusText(http.StatusInternalServerError), err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	} else if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		log.Printf("(%s) Host responded with %d (%s)", req.File, resp.StatusCode, http.StatusText(resp.StatusCode))
		http.Error(w, http.StatusText(resp.StatusCode), resp.StatusCode)
		return
	}
	defer resp.Body.Close()

	for _, h := range headersToForward {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}

	if req.FileStatus == fileStatusNoCaching {
		_, err = io.Copy(w, resp.Body)
		if err == nil {
			log.Printf("(%s) Uncacheable file served successfully", req.File)
		} else {
			log.Printf("(%s) Uncacheable file not served: %s", req.File, err)
		}
		return
	}

	req.CacheEntry.HTTPInfo = w.Header().Clone()
	close(req.CacheEntry.Started)

	// Failed writes to the client are ignored so that caching can continue
	// then reported at the end
	pw := util.NewPerfectWriter(w)
	_, err = io.Copy(io.MultiWriter(req.CacheEntry.File, pw), resp.Body)
	if err == nil {
		err = renameTempFile(req, resp.Header.Get("Last-Modified"))
	}

	switch {
	case err == nil && pw.Error() == nil:
		log.Printf("(%s) File cached and served successfully", req.File)
	case err == nil && pw.Error() != nil:
		log.Printf("(%s) File cached successfully, but serving to the client failed: %s", req.File, pw.Error())
	default:
		log.Printf("(%s) File caching failed: %s", req.File, err)
	}
}

func fileHandlerInDownload(w http.ResponseWriter, _ *http.Request, req *request) {
	log.Printf("(%s) Forwarding file in download", req.File)
	checkSizeTick := time.NewTicker(time.Second)
	var fileSize int64
	done := false
	var err error

	<-req.CacheEntry.Started

	for h, v := range req.CacheEntry.HTTPInfo {
		w.Header()[h] = v
	}

	for !done {
		select {
		case <-req.CacheEntry.Complete:
			done = true
		case <-checkSizeTick.C:
		}

		var newSize int64
		newSize, err := req.CacheEntry.File.Seek(0, io.SeekCurrent)
		if err != nil {
			break
		}
		if newSize <= fileSize {
			continue
		}
		_, err = io.Copy(w, io.NewSectionReader(req.CacheEntry.File, fileSize, newSize-fileSize))
		if err != nil {
			break
		}
		fileSize = newSize
	}

	if err != nil {
		log.Printf("(%s) Error while serving file in download: %s", req.File, err)
	} else {
		log.Printf("(%s) File in download served", req.File)
	}
}

func handleRequest(w http.ResponseWriter, r *http.Request, req *request) {
	var entry *cacheEntry
	var fileStatus fileStatus

	err := sharedState.LockedDo(func(cache map[string]*cacheEntry) error {
		_, err := os.Stat(req.GetCachePathName())
		switch {
		case strings.HasSuffix(req.File, ".db") || strings.HasSuffix(req.File, ".db.sig"):
			fileStatus = fileStatusNoCaching
		case errors.Is(err, os.ErrNotExist):
			// File does not exists, maybe it is being downloaded
			var isInDownload bool
			if entry, isInDownload = cache[req.File]; isInDownload {
				// File currently in download, request should be handled by tailing the file
				// until the download is complete
				entry.RefCount++
				fileStatus = fileStatusInDownload
			} else {
				// File not in download, the request must be handled by downloading the file
				entry, err = newCacheEntryFromRequest(req)
				if err != nil {
					return fmt.Errorf("unable to create cache file: %w", err)
				}
				cache[req.File] = entry
				fileStatus = fileStatusMissing
			}
		case err == nil:
			// File already downloaded, serve it
			fileStatus = fileStatusCached
		default:
			return fmt.Errorf("unable to stat cached file: %w", err)
		}
		return nil
	})
	if err != nil {
		log.Printf("(%s) Cache error: %s", req.File, err)
		return
	}

	cacheCleaner := func(cache map[string]*cacheEntry) error {
		if fileStatus == fileStatusMissing {
			close(entry.Complete)
		}
		entry.RefCount--
		if entry.RefCount == 0 {
			entry.File.Close()
			os.Remove(req.GetCacheTempPathName())
			delete(cache, req.File)
		}
		return nil
	}
	if entry != nil {
		defer sharedState.LockedDo(cacheCleaner)
	}

	req.CacheEntry = entry
	req.FileStatus = fileStatus

	fileHandlers[fileStatus](w, r, req)
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Request for URL: %s\n", r.URL)

	if r.Method != "GET" {
		log.Printf("We don't do %q, sending %q", r.Method, http.StatusText(http.StatusNotImplemented))
		http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
		return
	}

	req, err := newRequest(r.URL.String())
	if err != nil {
		log.Printf("URL invalid, sending %q", http.StatusText(http.StatusBadRequest))
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
	flKeepCache := flag.Bool("keep-cache", false, "Keep the cache between restarts")
	flag.Parse()

	if *flShowVersion {
		fmt.Printf("pkgproxy %s\n", version)
		return
	}

	if len(*flCachePath) > 0 {
		gSettings.CacheDir = *flCachePath
	} else {
		var err error
		gSettings.CacheDir, err = os.UserCacheDir()
		if err != nil {
			log.Fatalf("Unable to determine user cache directory: %s", err)
		}
	}
	gSettings.CacheDir = path.Join(gSettings.CacheDir, "pkgproxy")
	gSettings.UpstreamServer = *flUpstream

	var err error
	if *flKeepCache {
		err = setupCacheDir()
	} else {
		err = destroyCacheDir()
		if err == nil {
			err = setupCacheDir()
			defer destroyCacheDir()
		}
	}
	if err != nil {
		log.Fatalf("Unable to setup cache directory %s: %s", gSettings.CacheDir, err)
	}

	log.Printf(
		"pkgproxy %s listening on %s, forwarding to %s and storing to %s",
		version,
		*flAddr,
		gSettings.UpstreamServer,
		gSettings.CacheDir,
	)
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(*flAddr, nil))
}
