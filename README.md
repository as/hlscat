# hlscat

`hlscat` is a command line HLS playlist repackager experiment. The goal was to create a repackager that does not re-encode content or use any temporary files.

It handles `mpegts` segments and fragmented mp4s, as well as clearkey encryption (when the key is contained in the playlist). Given a master manifest on the command line, `hlscat` selects the best video and audio track and merges them together without reencoding, outputting the new stream to standard output without any temporary files.

`hlscat` has two primary functions: Listing and Repackaging.

## Listing

A master or media manifest can be listed with `hlscat`. 

```
hlscat -l https://test-streams.mux.dev/x36xhzz/x36xhzz.m3u8
/url_0/193039199_mp4_h264_aac_hd_7.m3u8
/url_2/193039199_mp4_h264_aac_ld_7.m3u8
/url_4/193039199_mp4_h264_aac_7.m3u8
/url_6/193039199_mp4_h264_aac_hq_7.m3u8
/url_8/193039199_mp4_h264_aac_fhd_7.m3u8

; hlscat -l https://test-streams.mux.dev/x36xhzz/url_8/193039199_mp4_h264_aac_fhd_7.m3u8 | head
base path is https://test-streams.mux.dev/x36xhzz/url_8/
media playlist duration=10m34.567s
url_590/193039199_mp4_h264_aac_fhd_7.ts	d=10.000000	t=10.000000
url_591/193039199_mp4_h264_aac_fhd_7.ts	d=10.000000	t=20.000000
url_592/193039199_mp4_h264_aac_fhd_7.ts	d=10.000000	t=30.000000
url_593/193039199_mp4_h264_aac_fhd_7.ts	d=10.000000	t=40.000000
url_594/193039199_mp4_h264_aac_fhd_7.ts	d=10.000000	t=50.000000
url_595/193039199_mp4_h264_aac_fhd_7.ts	d=10.000000	t=60.000000
url_596/193039199_mp4_h264_aac_fhd_7.ts	d=10.000000	t=70.000000
url_597/193039199_mp4_h264_aac_fhd_7.ts	d=9.933000	t=79.933000
url_598/193039199_mp4_h264_aac_fhd_7.ts	d=10.067000	t=90.000000
url_599/193039199_mp4_h264_aac_fhd_7.ts	d=10.000000	t=100.000000
```

The `-abs` flag will produce absolute paths and `-r` will recurse into media manifests from a master manifests.

```
hlscat -abs -l https://test-streams.mux.dev/x36xhzz/url_8/193039199_mp4_h264_aac_fhd_7.m3u8 | head

https://test-streams.mux.dev/x36xhzz/url_8/url_590/193039199_mp4_h264_aac_fhd_7.ts
https://test-streams.mux.dev/x36xhzz/url_8/url_591/193039199_mp4_h264_aac_fhd_7.ts
https://test-streams.mux.dev/x36xhzz/url_8/url_592/193039199_mp4_h264_aac_fhd_7.ts
https://test-streams.mux.dev/x36xhzz/url_8/url_593/193039199_mp4_h264_aac_fhd_7.ts
https://test-streams.mux.dev/x36xhzz/url_8/url_594/193039199_mp4_h264_aac_fhd_7.ts
https://test-streams.mux.dev/x36xhzz/url_8/url_595/193039199_mp4_h264_aac_fhd_7.ts
https://test-streams.mux.dev/x36xhzz/url_8/url_596/193039199_mp4_h264_aac_fhd_7.ts
https://test-streams.mux.dev/x36xhzz/url_8/url_597/193039199_mp4_h264_aac_fhd_7.ts
https://test-streams.mux.dev/x36xhzz/url_8/url_598/193039199_mp4_h264_aac_fhd_7.ts
https://test-streams.mux.dev/x36xhzz/url_8/url_599/193039199_mp4_h264_aac_fhd_7.ts
```

## Repackaging

### Muxed TS segment stream (audio+video in one container)

```
; hlscat  https://test-streams.mux.dev/x36xhzz/x36xhzz.m3u8 > av.mp4

hls video: https://test-streams.mux.dev/x36xhzz/url_8/193039199_mp4_h264_aac_fhd_7.m3u8
hls audio: https://test-streams.mux.dev/x36xhzz/url_8/193039199_mp4_h264_aac_fhd_7.m3u8
hls single-file a/v
media playlist duration=10m34.567s

; minfo av.mp4
{"Path":"av.mp4","StreamSize":"801278","Duration":"634.567","CodecID":"iso5","CodecID_Compatible":"iso5/iso6/mp41","Format":"MPEG-4","Format_Profile":"Base Media","AudioCount":"1","FrameCount":"38074","VideoCount":"1","DataSize":"5125874","FileSize":"487585690","FooterSize":"482445342","HeaderSize":"14474","Encoded_Application":"Lavf59.34.101","OverallBitRate":"6147003","IsStreamable":"Yes",
	"Track": [
{"@type":"Video","ID":"1","StreamSize":"474736434","Duration":"634.567","FrameCount":"38074","FrameRate":"60","FrameRate_Mode":"VFR","CodecID":"avc1","Format":"AVC","Format_Profile":"High","Format_Level":"4","Format_Settings_RefFrames":"5","Format_Settings_CABAC":"Yes","BitRate":"6000000","Width":"1920","Height":"1080","DisplayAspectRatio":"1.778","PixelAspectRatio":"1","BitDepth":"8","ScanType":"Progressive","ColorSpace":"YUV","ChromaSubsampling":"4:2:0"},
		{"@type":"Audio","ID":"2","StreamOrder":"1","StreamSize":"12047978","Duration":"634.194","FrameCount":"27312","FrameRate":"7.178","CodecID":"mp4a-40-27","Format":"ER Parametric","BitRate":"153725","BitRate_Mode":"CBR","SamplingCount":"4661326","SamplingRate":"7350","SamplesPerFrame":"1024","AlternateGroup":"1","Default":"Yes"}]}
```

### Late bound audio stream (audio and video in seperate container)

(same command as above, I don't have any public examples of such a playlist)

### DRM (AES-128-CBC Clearkey Encryption)

```
hlscat https://test-streams.mux.dev/dai-discontinuity-deltatre/manifest.m3u8 > av.mp4
```

### AD Removal

There are many ways to signal an AD break in a playlist so detecting one methodically can be difficult. For the purpose of this program, an AD is any segment that contains an EXT-CUE-OUT or EXT-CUE-OUT-CONT tag.

```
hlscat -noads $URL > av.mp4
```


### Blackframe Insertion

This feature, instead of removing ADs, covers them with black frames and silent audio. Currently this feature does not work properly unless characteristic of the encoded stream are defined correctly on the command line using the z-variables. Using it or relying on it to produce stable output is not recommended.

```
hlscat -blackframe $URL > av.mp4
```
