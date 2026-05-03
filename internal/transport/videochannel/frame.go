package videochannel

import (
	"encoding/binary"
	"errors"
)

const (
	protocolMagic   uint32 = 0x4f565632 // OVV2
	protocolVersion byte   = 1
	frameTypeData   byte   = 1
	frameTypeAck    byte   = 2
)

var (
	// ErrFrameTooShort is returned when the received frame is too short to decode.
	ErrFrameTooShort = errors.New("frame too short")
	// ErrUnexpectedMagic is returned when the frame magic bytes do not match.
	ErrUnexpectedMagic = errors.New("unexpected frame magic")
	// ErrUnexpectedVersion is returned when the frame protocol version does not match.
	ErrUnexpectedVersion = errors.New("unexpected frame version")
	// ErrAckTooShort is returned when the ack frame is shorter than expected.
	ErrAckTooShort = errors.New("ack frame too short")
	// ErrDataTooShort is returned when the data frame is shorter than expected.
	ErrDataTooShort = errors.New("data frame too short")
	// ErrUnexpectedFrameType is returned for unknown frame type bytes.
	ErrUnexpectedFrameType = errors.New("unexpected frame type")
)

type transportFrame struct {
	typ       byte
	seq       uint32
	crc       uint32
	totalLen  uint32
	fragIdx   uint16
	fragTotal uint16
	payload   []byte
}

type inboundMessage struct {
	totalLen uint32
	crc      uint32
	frags    [][]byte
	remain   int
}

func fragmentPayload(data []byte, maxSize int) [][]byte {
	if len(data) == 0 {
		return [][]byte{{}}
	}

	out := make([][]byte, 0, (len(data)+maxSize-1)/maxSize)
	for start := 0; start < len(data); start += maxSize {
		end := start + maxSize
		if end > len(data) {
			end = len(data)
		}

		chunk := make([]byte, end-start)
		copy(chunk, data[start:end])
		out = append(out, chunk)
	}

	return out
}

func encodeDataFrame(seq, crc uint32, totalLen, fragIdx, fragTotal int, payload []byte) []byte {
	out := make([]byte, 22+len(payload))
	binary.BigEndian.PutUint32(out[0:4], protocolMagic)
	out[4] = protocolVersion
	out[5] = frameTypeData
	binary.BigEndian.PutUint32(out[6:10], seq)
	binary.BigEndian.PutUint32(out[10:14], crc)
	binary.BigEndian.PutUint32(out[14:18], uint32(totalLen))  //nolint:gosec
	binary.BigEndian.PutUint16(out[18:20], uint16(fragIdx))   //nolint:gosec
	binary.BigEndian.PutUint16(out[20:22], uint16(fragTotal)) //nolint:gosec
	copy(out[22:], payload)
	return out
}

func encodeAckFrame(seq, crc uint32) []byte {
	out := make([]byte, 14)
	binary.BigEndian.PutUint32(out[0:4], protocolMagic)
	out[4] = protocolVersion
	out[5] = frameTypeAck
	binary.BigEndian.PutUint32(out[6:10], seq)
	binary.BigEndian.PutUint32(out[10:14], crc)
	return out
}

func decodeTransportFrame(data []byte) (transportFrame, error) {
	if len(data) < 6 {
		return transportFrame{}, ErrFrameTooShort
	}
	if binary.BigEndian.Uint32(data[0:4]) != protocolMagic {
		return transportFrame{}, ErrUnexpectedMagic
	}
	if data[4] != protocolVersion {
		return transportFrame{}, ErrUnexpectedVersion
	}

	frame := transportFrame{typ: data[5]}
	switch frame.typ {
	case frameTypeAck:
		if len(data) < 14 {
			return transportFrame{}, ErrAckTooShort
		}
		frame.seq = binary.BigEndian.Uint32(data[6:10])
		frame.crc = binary.BigEndian.Uint32(data[10:14])
		return frame, nil
	case frameTypeData:
		if len(data) < 22 {
			return transportFrame{}, ErrDataTooShort
		}
		frame.seq = binary.BigEndian.Uint32(data[6:10])
		frame.crc = binary.BigEndian.Uint32(data[10:14])
		frame.totalLen = binary.BigEndian.Uint32(data[14:18])
		frame.fragIdx = binary.BigEndian.Uint16(data[18:20])
		frame.fragTotal = binary.BigEndian.Uint16(data[20:22])
		frame.payload = append([]byte(nil), data[22:]...)
		return frame, nil
	default:
		return transportFrame{}, ErrUnexpectedFrameType
	}
}
