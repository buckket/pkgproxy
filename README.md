[![Build Status](https://drone.buckket.org/api/badges/buckket/pkgproxy/status.svg)](https://drone.buckket.org/buckket/pkgproxy)

**pkgproxy** is a caching proxy server specifically designed for caching Arch GNU/Linux packages for pacman.

Updating multiple Arch systems in your home network can be a slow process if you have to download every pkg file
for every machine over and over again. One could setup a local Arch Linux mirror, but it takes a considerable amount of
disk space (~60GB). Instead why not just cache packages you really downloaded on one machine since it’s highly likely that
other computers will need to update the same packages. That’s exactly what pkgproxy does. It relays pacmans HTTP requests
and saves a copy to disk so that future requests of the same file can be served from the local cache.

## Installation

### From source

    go get -u git.buckket.org/buckket/pkgproxy

### Packet manager

- Arch Linux: [pkgproxy](https://aur.archlinux.org/packages/pkgproxy/)<sup>AUR</sup>

## Usage

Update your clients mirror list (`/etc/pacman.d/mirrorlist`) to point to `pkgproxy`:
  
    Server = http://${HOST_WITH_PKGPROXY_RUNNING}:8080/$repo/os/$arch
 
Run `pkgproxy` manually or use a systemd service file (example provided):

```
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
```

## Limitations

- Multiple incoming requests of the same file are handled sequentially, which may cause pacman to timeout,
  especially if a large file is being downloaded.
- All cached files are deleted when `pkgproxy` exits. No files will be deleted by `pkgproxy` as long as
  it is running. If you want to limit disk usage create a systemd timer which deletes files older than x days.

## License

 GNU GPLv3+
 