package main

import "testing"

func TestBuildUpstreamURL(t *testing.T) {
	GSettings.UpstreamServer = "https://example.org/pub/archlinux/$repo/os/$arch"

	req := Request{"extra", "os", "x86_64", "extra.db"}
	url := buildUpstreamURL(&req)
	if url != "https://example.org/pub/archlinux/extra/os/x86_64/extra.db" {
		t.Error("URL does not match")
	}

	req = Request{}
	url = buildUpstreamURL(&req)
	if url != "https://example.org/pub/archlinux//os//" {
		t.Error("URL does not match")
	}
}

func TestSplitReqURL(t *testing.T) {
	url, err := splitReqURL("/extra/os/x86_64/abiword-3.0.2-9-x86_64.pkg.tar.xz")
	if err != nil {
		t.Error("Parsing URL failed")
	} else if url.Repo != "extra" || url.OS != "os" || url.Arch != "x86_64" || url.File != "abiword-3.0.2-9-x86_64.pkg.tar.xz" {
		t.Error("Parsed URL does not match expected result")
	}

	url, err = splitReqURL("")
	if err == nil {
		t.Error("Parsing URL should have failed")
	}
}
