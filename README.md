**pkgproxy** is a caching proxy server specifically designed for caching Arch GNU/Linux packages for pacman.

_This is a major rewrite of https://github.com/buckket/pkgproxy in order to iron out some bugs and implement
concurrent downloading of the same uncached file. It can be used as a drop-in replacement of the original
`pkgproxy`, with the exception that is does not caches databases (which is transparent anyway)._


Updating multiple Arch systems in your home network can be a slow process if you have to download every pkg file
for every machine over and over again. One could setup a local Arch Linux mirror, but it takes a considerable amount of
disk space (~60GB). Instead why not just cache packages you really downloaded on one machine since it’s highly likely that
other computers will need to update the same packages. That’s exactly what pkgproxy does. It relays pacmans HTTP requests
and saves a copy to disk so that future requests of the same file can be served from the local cache.

## Installation

    go install github.com/binary-manu/pkgproxy/cmd/pkgproxy@latest

## Usage

Update your clients mirror list (`/etc/pacman.d/mirrorlist`) to point to `pkgproxy`:
  
    Server = http://${HOST_WITH_PKGPROXY_RUNNING}:8080/$repo/os/$arch
 
Run `pkgproxy` manually or use a systemd service file.

```
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
```

## Things to know

- Database files are not cached.
- Packages are cached, and can be downloaded concurrently: there is no blocking while a package is being
  stored into the cache.
- All cached files are deleted when `pkgproxy` exits, unless `-keep-cache` is used.

## License

 GNU GPLv3+
 
