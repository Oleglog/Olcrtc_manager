package videochannel

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

var (
	errVideoFrameBase = errors.New("base")
	errVideoFrameBoom = errors.New("boom")
)

func TestFragmentPayload(t *testing.T) {
	frags := fragmentPayload([]byte("abcdef"), 2)
	want := [][]byte{[]byte("ab"), []byte("cd"), []byte("ef")}
	if len(frags) != len(want) {
		t.Fatalf("fragment count = %d, want %d", len(frags), len(want))
	}
	for i := range frags {
		if !bytes.Equal(frags[i], want[i]) {
			t.Fatalf("frag %d = %q, want %q", i, frags[i], want[i])
		}
	}

	empty := fragmentPayload(nil, 10)
	if len(empty) != 1 || len(empty[0]) != 0 {
		t.Fatalf("fragmentPayload(nil) = %#v, want one empty frag", empty)
	}
}

func TestDecodeTransportFrameErrorsAndAck(t *testing.T) {
	tests := []struct {
		data []byte
		want error
	}{
		{data: []byte{1, 2, 3}, want: ErrFrameTooShort},
		{data: []byte{0, 0, 0, 0, protocolVersion, frameTypeAck}, want: ErrUnexpectedMagic},
		{data: []byte{0x4f, 0x56, 0x56, 0x32, 9, frameTypeAck}, want: ErrUnexpectedVersion},
		{data: []byte{0x4f, 0x56, 0x56, 0x32, protocolVersion, frameTypeAck}, want: ErrAckTooShort},
		{data: []byte{0x4f, 0x56, 0x56, 0x32, protocolVersion, frameTypeData}, want: ErrDataTooShort},
		{data: []byte{0x4f, 0x56, 0x56, 0x32, protocolVersion, 99}, want: ErrUnexpectedFrameType},
	}
	for _, tt := range tests {
		if _, err := decodeTransportFrame(tt.data); !errors.Is(err, tt.want) {
			t.Fatalf("decodeTransportFrame(%v) error = %v, want %v", tt.data, err, tt.want)
		}
	}

	ack, err := decodeTransportFrame(encodeAckFrame(7, 0x1234))
	if err != nil {
		t.Fatalf("decode ack error = %v", err)
	}
	if ack.typ != frameTypeAck || ack.seq != 7 || ack.crc != 0x1234 {
		t.Fatalf("ack = %+v", ack)
	}
}

//nolint:cyclop // table-driven test naturally has many branches
func TestCodecSpecsAndArgs(t *testing.T) {
	for _, mime := range []string{webrtc.MimeTypeH264, webrtc.MimeTypeVP8, webrtc.MimeTypeVP9} {
		spec, ok := codecSpecForMime(mime)
		if !ok {
			t.Fatalf("codecSpecForMime(%q) ok = false", mime)
		}
		if spec.mimeType != mime || spec.depacketizer == nil || spec.capability.ClockRate != 90000 {
			t.Fatalf("codec spec = %+v", spec)
		}
	}
	if _, ok := codecSpecForMime("video/unknown"); ok {
		t.Fatal("codecSpecForMime() accepted unknown mime")
	}

	if got := resolveEncoderCodec(h264CodecSpec(), "nvenc"); got != "h264_nvenc" {
		t.Fatalf("resolveEncoderCodec(h264,nvenc) = %q", got)
	}
	if got := resolveEncoderCodec(vp8CodecSpec(), "none"); got != "libvpx" {
		t.Fatalf("resolveEncoderCodec(vp8,none) = %q", got)
	}
	if got := resolveEncoderCodec(vp9CodecSpec(), "nvenc"); got != "vp9_nvenc" {
		t.Fatalf("resolveEncoderCodec(vp9,nvenc) = %q", got)
	}
	if got := resolveEncoderCodec(codecSpec{mimeType: webrtc.MimeTypeAV1, encoder: "libaom-av1"}, "nvenc"); got != "av1_nvenc" { //nolint:lll // long test description
		t.Fatalf("resolveEncoderCodec(av1,nvenc) = %q", got)
	}

	args := buildEncoderArgs(vp8CodecSpec(), "vp8_nvenc", 320, 240, 30, "1M")
	for _, want := range []string{"-video_size", "320x240", "-framerate", "30", "vp8_nvenc", "-b:v", "1M", "ivf"} {
		if !slices.Contains(args, want) {
			t.Fatalf("buildEncoderArgs() = %v, missing %q", args, want)
		}
	}
	h264Args := buildEncoderArgs(h264CodecSpec(), "libx264", 320, 240, 30, "1M")
	if h264Args[len(h264Args)-2] != "h264" {
		t.Fatalf("h264 encoder args = %v", h264Args)
	}

	if got := resolveDecoderName(h264CodecSpec(), "nvenc"); got != "h264_cuvid" {
		t.Fatalf("resolveDecoderName(h264,nvenc) = %q", got)
	}
	if got := resolveDecoderName(vp8CodecSpec(), "nvenc"); got != "vp8_cuvid" {
		t.Fatalf("resolveDecoderName(vp8,nvenc) = %q", got)
	}
	if got := resolveDecoderName(vp9CodecSpec(), "nvenc"); got != "vp9_cuvid" {
		t.Fatalf("resolveDecoderName(vp9,nvenc) = %q", got)
	}
	if got := resolveDecoderName(codecSpec{mimeType: "video/custom"}, "none"); got != "custom" {
		t.Fatalf("resolveDecoderName(custom,none) = %q", got)
	}
	decArgs := buildDecoderArgs(vp8CodecSpec(), "vp8", 320, 240, "gray")
	for _, want := range []string{"-f", "ivf", "-vcodec", "vp8", "scale=320:240:flags=neighbor,format=gray", "rawvideo"} {
		if !slices.Contains(decArgs, want) {
			t.Fatalf("buildDecoderArgs(vp8) = %v, missing %q", decArgs, want)
		}
	}
	h264DecArgs := buildDecoderArgs(h264CodecSpec(), "h264", 320, 240, "gray")
	if h264DecArgs[5] != "h264" {
		t.Fatalf("buildDecoderArgs(h264) = %v", h264DecArgs)
	}
}

type shortWriter struct {
	writes int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == 1 {
		return 1, nil
	}
	return len(p), nil
}

type errWriter struct{}

func (w errWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type bufferWriteCloser struct {
	bytes.Buffer
}

func (w *bufferWriteCloser) Close() error { return nil }

//nolint:cyclop // table-driven test naturally has many branches
func TestIVFWritersAndWithStderr(t *testing.T) {
	var buf bytes.Buffer
	if err := writeIVFHeader(&buf, "VP80", 320, 240, 30); err != nil {
		t.Fatalf("writeIVFHeader() error = %v", err)
	}
	if buf.Len() != 32 || string(buf.Bytes()[:4]) != "DKIF" {
		t.Fatalf("IVF header = %v", buf.Bytes())
	}

	buf.Reset()
	if err := writeIVFFrame(&buf, 3, []byte("abc")); err != nil {
		t.Fatalf("writeIVFFrame() error = %v", err)
	}
	if buf.Len() != 15 {
		t.Fatalf("IVF frame len = %d, want 15", buf.Len())
	}

	if err := writeAll(&shortWriter{}, []byte("abc")); err != nil {
		t.Fatalf("writeAll(shortWriter) error = %v", err)
	}
	if err := writeAll(errWriter{}, []byte("abc")); err == nil || !strings.Contains(err.Error(), "write:") {
		t.Fatalf("writeAll(errWriter) error = %v", err)
	}

	baseErr := errVideoFrameBase
	if got := withStderr(baseErr, bytes.NewBufferString(" details \n")); got == nil || got.Error() != "base: details" {
		t.Fatalf("withStderr() = %v", got)
	}
	if got := withStderr(nil, bytes.NewBufferString("details")); got != nil {
		t.Fatalf("withStderr(nil) = %v", got)
	}
}

func TestFFmpegProcessErrAndFrameValidation(t *testing.T) {
	enc := &ffmpegEncoder{
		stderr:    bytes.NewBufferString("encoder failed"),
		frames:    make(chan []byte, 1),
		frameSize: 4,
	}
	if _, err := enc.EncodeFrame([]byte("bad")); !errors.Is(err, ErrUnexpectedFrameSize) {
		t.Fatalf("EncodeFrame(short) error = %v, want %v", err, ErrUnexpectedFrameSize)
	}
	enc.setErr(errVideoFrameBoom)
	if _, err := enc.EncodeFrame([]byte("good")); err == nil || !strings.Contains(err.Error(), "encoder failed") {
		t.Fatalf("EncodeFrame(processErr) error = %v", err)
	}

	dec := &ffmpegDecoder{stderr: bytes.NewBufferString("decoder failed")}
	dec.setErr(errVideoFrameBoom)
	if err := dec.PushSample([]byte("sample")); err == nil || !strings.Contains(err.Error(), "decoder failed") {
		t.Fatalf("PushSample(processErr) error = %v", err)
	}
	closed := &ffmpegDecoder{}
	closed.closed.Store(true)
	if err := closed.processErr(); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("decoder processErr(closed) = %v, want %v", err, ErrTransportClosed)
	}
}

//nolint:cyclop // table-driven test naturally has many branches
func TestFFmpegReadersAndSampleWriters(t *testing.T) {
	var ivf bytes.Buffer
	if err := writeIVFHeader(&ivf, "VP80", 2, 2, 30); err != nil {
		t.Fatalf("writeIVFHeader() error = %v", err)
	}
	if err := writeIVFFrame(&ivf, 1, []byte("frame")); err != nil {
		t.Fatalf("writeIVFFrame() error = %v", err)
	}
	enc := &ffmpegEncoder{stderr: &bytes.Buffer{}, frames: make(chan []byte, 2)}
	enc.readIVF(&ivf)
	if got := <-enc.frames; !bytes.Equal(got, []byte("frame")) {
		t.Fatalf("readIVF frame = %q", got)
	}

	enc = &ffmpegEncoder{stderr: &bytes.Buffer{}, frames: make(chan []byte, 2)}
	enc.readRawH264(bytes.NewBufferString("h264"))
	if got := <-enc.frames; !bytes.Equal(got, []byte("h264")) {
		t.Fatalf("readRawH264 frame = %q", got)
	}

	dec := &ffmpegDecoder{stderr: &bytes.Buffer{}, frames: make(chan []byte, 2), frameSize: 4}
	dec.readRawFrames(bytes.NewBufferString("aaaabbbb"))
	if got := <-dec.frames; !bytes.Equal(got, []byte("aaaa")) {
		t.Fatalf("readRawFrames first = %q", got)
	}
	if got := <-dec.frames; !bytes.Equal(got, []byte("bbbb")) {
		t.Fatalf("readRawFrames second = %q", got)
	}

	h264In := &bufferWriteCloser{}
	dec = &ffmpegDecoder{stdin: h264In, mimeType: webrtc.MimeTypeH264}
	if err := dec.PushSample([]byte("sample")); err != nil {
		t.Fatalf("PushSample(h264) error = %v", err)
	}
	if h264In.String() != "sample" {
		t.Fatalf("h264 stdin = %q", h264In.String())
	}

	ivfIn := &bufferWriteCloser{}
	dec = &ffmpegDecoder{stdin: ivfIn, mimeType: webrtc.MimeTypeVP8}
	if err := dec.PushSample([]byte("vp8")); err != nil {
		t.Fatalf("PushSample(vp8) error = %v", err)
	}
	if ivfIn.Len() != 12+len("vp8") || dec.pts != 1 {
		t.Fatalf("ivf stdin len=%d pts=%d", ivfIn.Len(), dec.pts)
	}

	dec = &ffmpegDecoder{frames: make(chan []byte, 1)}
	dec.frames <- []byte("ready")
	if got, err := dec.PopFrame(); err != nil || !bytes.Equal(got, []byte("ready")) {
		t.Fatalf("PopFrame() = %q, %v", got, err)
	}
}
