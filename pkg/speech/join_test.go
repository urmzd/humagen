package speech_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/speech"
)

// wav is a mono 16-bit WAV at rate with the samples given, its data size
// written as size (0 writes the real one).
func wav(rate uint32, samples []byte, size uint32) []byte {
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	le(uint32(36 + len(samples)))
	b.WriteString("WAVEfmt ")
	le(uint32(16))
	le(uint16(1))
	le(uint16(1))
	le(rate)
	le(rate * 2)
	le(uint16(2))
	le(uint16(16))
	b.WriteString("data")
	if size == 0 {
		size = uint32(len(samples))
	}
	le(size)
	b.Write(samples)
	return b.Bytes()
}

func TestClipsJoinAsOne(t *testing.T) {
	joined, err := speech.Join("audio/mpeg", [][]byte{[]byte("ID3a"), []byte("ID3b")})
	if err != nil || string(joined) != "ID3aID3b" {
		t.Fatalf("mp3: %q %v", joined, err)
	}
	// A streamed WAV, whose data size is unknown, joins too.
	joined, err = speech.Join("audio/wav", [][]byte{wav(24000, []byte{1, 2, 3, 4}, 0), wav(24000, []byte{5, 6}, 0xFFFFFFFF)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(joined, wav(24000, []byte{1, 2, 3, 4, 5, 6}, 0)) {
		t.Fatalf("wav joined to % x", joined)
	}
	if _, err := speech.Join("audio/wav", [][]byte{wav(24000, []byte{1, 2}, 0), wav(16000, []byte{3, 4}, 0)}); !errors.Is(err, speech.ErrUnsupported) {
		t.Fatalf("two formats: %v", err)
	}
	if _, err := speech.Join("audio/wav", [][]byte{[]byte("nope"), wav(24000, nil, 0)}); !errors.Is(err, speech.ErrUnsupported) {
		t.Fatalf("not a wav: %v", err)
	}
	if one, _ := speech.Join("audio/ogg", [][]byte{[]byte("x")}); string(one) != "x" {
		t.Fatal("one clip is itself")
	}
	if _, err := speech.Join("audio/mpeg", nil); !errors.Is(err, speech.ErrEmpty) {
		t.Fatal("no clips")
	}
}
