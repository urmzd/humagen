package speech

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Join puts clips of one media type end to end as one clip. MP3 is a run of
// frames, so its clips join as they are; WAV clips must share a format, and
// join as one header over their samples.
func Join(mediaType string, clips [][]byte) ([]byte, error) {
	switch len(clips) {
	case 0:
		return nil, ErrEmpty
	case 1:
		return clips[0], nil
	}
	switch BaseType(mediaType) {
	case "audio/mpeg", "audio/mp3":
		return bytes.Join(clips, nil), nil
	case "audio/wav", "audio/x-wav", "audio/wave":
		return joinWAV(clips)
	}
	return nil, fmt.Errorf("%w: cannot join %q", ErrUnsupported, mediaType)
}

// wavParts is a WAV file's format chunk and its samples.
func wavParts(b []byte) (format, data []byte, err error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, nil, fmt.Errorf("%w: not a WAV file", ErrUnsupported)
	}
	for i := 12; i+8 <= len(b); {
		id, size := string(b[i:i+4]), int(binary.LittleEndian.Uint32(b[i+4:i+8]))
		body := b[i+8:]
		// A streamed WAV says its data runs to the end, whatever its size field.
		if id == "data" && (size > len(body) || size == 0 || size == 0xFFFFFFFF) {
			size = len(body)
		}
		if size > len(body) {
			return nil, nil, fmt.Errorf("%w: a WAV chunk runs past the end", ErrUnsupported)
		}
		switch id {
		case "fmt ":
			format = body[:size]
		case "data":
			data = body[:size]
		}
		i += 8 + size + size%2
	}
	if format == nil || data == nil {
		return nil, nil, fmt.Errorf("%w: a WAV file with no format or no samples", ErrUnsupported)
	}
	return format, data, nil
}

func joinWAV(clips [][]byte) ([]byte, error) {
	var format []byte
	var data bytes.Buffer
	for _, c := range clips {
		f, d, err := wavParts(c)
		if err != nil {
			return nil, err
		}
		if format == nil {
			format = f
		} else if !bytes.Equal(f, format) {
			return nil, fmt.Errorf("%w: WAV clips in different formats", ErrUnsupported)
		}
		data.Write(d)
	}
	var out bytes.Buffer
	le := func(v uint32) { _ = binary.Write(&out, binary.LittleEndian, v) }
	out.WriteString("RIFF")
	le(uint32(4 + 8 + len(format) + len(format)%2 + 8 + data.Len()))
	out.WriteString("WAVE")
	out.WriteString("fmt ")
	le(uint32(len(format)))
	out.Write(format)
	if len(format)%2 == 1 {
		out.WriteByte(0)
	}
	out.WriteString("data")
	le(uint32(data.Len()))
	out.Write(data.Bytes())
	return out.Bytes(), nil
}
