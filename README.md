# hlscat

`hlscat` is a command line HLS playlist repackager. It handles `mpegts` segments and fragmented mp4s, as well as clearkey encryption (when the key is contained in the playlist). Given a master manifest on the command line, `hlscat` selects the best video and audio track and merges them together without reencoding, outputting the new stream to standard output without any temporary files.

```
hlscat -l https://test-streams.mux.dev/x36xhzz/x36xhzz.m3u8
/url_0/193039199_mp4_h264_aac_hd_7.m3u8
/url_2/193039199_mp4_h264_aac_ld_7.m3u8
/url_4/193039199_mp4_h264_aac_7.m3u8
/url_6/193039199_mp4_h264_aac_hq_7.m3u8
/url_8/193039199_mp4_h264_aac_fhd_7.m3u8
```

# Download a muxed TS segment stream

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

# Download a stream with late bound audio

(same command as above)
