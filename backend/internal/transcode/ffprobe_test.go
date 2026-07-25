package transcode

import "testing"

func TestParseFFprobeOutput(t *testing.T) {
	const fixture = `{
		"format": {"duration": "1425.5"},
		"streams": [
			{"codec_type": "video", "codec_name": "h264", "width": 1920, "height": 1080},
			{"codec_type": "audio", "codec_name": "aac", "channels": 2, "tags": {"language": "eng", "title": "Stereo"}},
			{"codec_type": "audio", "codec_name": "ac3", "channels": 6, "tags": {"language": "jpn"}}
		],
		"chapters": [
			{"start_time": "0.0", "end_time": "90.0", "tags": {"title": "Intro"}},
			{"start_time": "90.0", "end_time": "1425.5", "tags": {"title": "Episode"}}
		]
	}`

	info, err := parseFFprobeOutput([]byte(fixture))
	if err != nil {
		t.Fatalf("parseFFprobeOutput() error = %v", err)
	}

	if info.DurationSeconds != 1425.5 {
		t.Errorf("DurationSeconds = %v, want 1425.5", info.DurationSeconds)
	}
	if info.VideoCodec != "h264" || info.Width != 1920 || info.Height != 1080 {
		t.Errorf("video info = %+v, want h264 1920x1080", info)
	}
	if info.AudioCodec != "aac" {
		t.Errorf("AudioCodec = %q, want the first audio stream's codec (aac)", info.AudioCodec)
	}

	if len(info.AudioTracks) != 2 {
		t.Fatalf("len(AudioTracks) = %d, want 2", len(info.AudioTracks))
	}
	want := []AudioTrackInfo{
		{Index: 0, Codec: "aac", Language: "eng", Channels: 2, Title: "Stereo"},
		{Index: 1, Codec: "ac3", Language: "jpn", Channels: 6, Title: ""},
	}
	for i, w := range want {
		if info.AudioTracks[i] != w {
			t.Errorf("AudioTracks[%d] = %+v, want %+v", i, info.AudioTracks[i], w)
		}
	}

	if len(info.Chapters) != 2 {
		t.Fatalf("len(Chapters) = %d, want 2", len(info.Chapters))
	}
	if info.Chapters[0] != (ChapterInfo{StartSeconds: 0, EndSeconds: 90, Title: "Intro"}) {
		t.Errorf("Chapters[0] = %+v, want Intro chapter 0-90", info.Chapters[0])
	}
}

func TestParseFFprobeOutput_NoChaptersOrExtraTracks(t *testing.T) {
	const fixture = `{
		"format": {"duration": "600"},
		"streams": [
			{"codec_type": "video", "codec_name": "h264"},
			{"codec_type": "audio", "codec_name": "aac"}
		]
	}`

	info, err := parseFFprobeOutput([]byte(fixture))
	if err != nil {
		t.Fatalf("parseFFprobeOutput() error = %v", err)
	}
	if len(info.Chapters) != 0 {
		t.Errorf("Chapters = %+v, want empty for a file with no chapter markers", info.Chapters)
	}
	if len(info.AudioTracks) != 1 || info.AudioTracks[0].Index != 0 {
		t.Errorf("AudioTracks = %+v, want a single track at index 0", info.AudioTracks)
	}
}
