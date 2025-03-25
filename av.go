package main

type Info struct {
	Name, Container string  `json:",omitempty"`
	Size            int     `json:",omitempty"`
	Dur             float64 `json:",omitempty"`
	Video           Video   `json:",omitempty"`
	Audio           Audio   `json:",omitempty"`
	StreamOrder     string  `json:",omitempty"`
}
type Audio struct {
	Codec, Lang                   string `json:",omitempty"`
	Bitrate, Samplerate, Channels int    `json:",omitempty"`
}
type Video struct {
	Codec, Preset, Profile, Level, Chroma, Scantype string  `json:",omitempty"`
	Width, Height                                   int     `json:",omitempty"`
	FPS                                             float64 `json:",omitempty"`
	Bitrate                                         Bitrate `json:",omitempty"`
	Gop                                             Gop     `json:",omitempty"`
}
type Bitrate struct {
	BPS, CRF, Min, Max, Buf int `json:",omitempty"`
}
type Gop struct {
	Size           float64 `json:",omitempty"`
	Unit           string  `json:",omitempty"`
	Refs, Scenecut int     `json:",omitempty"`
	Open           bool    `json:",omitempty"`
}

func (v *Video) On() bool {
	return v != nil && !(v.Codec == "" && v.Height == 0 && v.Width == 0)
}
func (a *Audio) On() bool {
	return a != nil && a.Codec != ""
}
