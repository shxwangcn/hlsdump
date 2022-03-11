# hlsdump

This tool is used to dump hls media (vod or live) resources(include m3u8 and segments) to local directory.

Supported:

- Both Vod and Live.
- Master playlist which includes multiple variants( `#EXT-X-STREAM-INF` )

Not Supported yet:

- Data-Range segments;
- `#EXT-X-MEDIA` defined rendition;
- Encrypted segments

## usage

```sh
$ go build
$ ./hlsdump http://path/to/stream.m3u8 saved_dir
$ ls saved_dir
index.m3u8 1.ts 2.ts 3.ts
```
