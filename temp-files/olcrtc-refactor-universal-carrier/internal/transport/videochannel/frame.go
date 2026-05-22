package videochannel

import (
	"encoding/binary"
	"errors"
)

const (
	protocolMagic   uint32 = 0x4f565632 // OVV2
	protocolVersion byte   = 2
	frameTypeData   byte   = 1
	frameTypeAck    byte   = 2
	frameRoleAny    byte   = 0
	frameRoleServer byte   = 1
	frameRoleClient byte   = 2

	frameBindingOff   = 7
	frameSeqOff       = 11
	frameCRCOff       = 15
	frameAckLen       = 19
	frameTotalLenOff  = 19
	frameFragIdxOff   = 23
	frameFragTotalOff = 25
	frameDataHdrLen   = 27
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
	role      byte
	binding   uint32
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

func encodeDataFrameForBinding(
	role byte,
	binding uint32,
	seq, crc uint32,
	totalLen, fragIdx, fragTotal int,
	payload []byte,
) []byte {
	out := make([]byte, frameDataHdrLen+len(payload))
	binary.BigEndian.PutUint32(out[0:4], protocolMagic)
	out[4] = protocolVersion
	out[5] = frameTypeData
	out[6] = role
	binary.BigEndian.PutUint32(out[frameBindingOff:frameSeqOff], binding)
	binary.BigEndian.PutUint32(out[frameSeqOff:frameCRCOff], seq)
	binary.BigEndian.PutUint32(out[frameCRCOff:frameAckLen], crc)
	binary.BigEndian.PutUint32(out[frameTotalLenOff:frameFragIdxOff], uint32(totalLen))   //nolint:gosec,lll // G115: bounded conversion verified by surrounding logic
	binary.BigEndian.PutUint16(out[frameFragIdxOff:frameFragTotalOff], uint16(fragIdx))   //nolint:gosec,lll // G115: bounded conversion verified by surrounding logic
	binary.BigEndian.PutUint16(out[frameFragTotalOff:frameDataHdrLen], uint16(fragTotal)) //nolint:gosec,lll // G115: bounded conversion verified by surrounding logic
	copy(out[frameDataHdrLen:], payload)
	return out
}

func encodeAckFrame(seq, crc uint32) []byte {
	return encodeAckFrameForBinding(frameRoleAny, 0, seq, crc)
}

func encodeAckFrameForBinding(role byte, binding, seq, crc uint32) []byte {
	out := make([]byte, frameAckLen)
	binary.BigEndian.PutUint32(out[0:4], protocolMagic)
	out[4] = protocolVersion
	out[5] = frameTypeAck
	out[6] = role
	binary.BigEndian.PutUint32(out[frameBindingOff:frameSeqOff], binding)
	binary.BigEndian.PutUint32(out[frameSeqOff:frameCRCOff], seq)
	binary.BigEndian.PutUint32(out[frameCRCOff:frameAckLen], crc)
	return out
}

func decodeTransportFrame(data []byte) (transportFrame, error) {
	if err := validateFrameHeader(data); err != nil {
		return transportFrame{}, err
	}

	frame := transportFrame{typ: data[5]}
	if len(data) < frameSeqOff {
		return transportFrame{}, shortFrameError(frame.typ)
	}
	frame.role = data[6]
	frame.binding = binary.BigEndian.Uint32(data[frameBindingOff:frameSeqOff])

	switch frame.typ {
	case frameTypeAck:
		return decodeAckBody(frame, data)
	case frameTypeData:
		return decodeDataBody(frame, data)
	default:
		return transportFrame{}, ErrUnexpectedFrameType
	}
}

func validateFrameHeader(data []byte) error {
	if len(data) < 6 {
		return ErrFrameTooShort
	}
	if binary.BigEndian.Uint32(data[0:4]) != protocolMagic {
		return ErrUnexpectedMagic
	}
	if data[4] != protocolVersion {
		return ErrUnexpectedVersion
	}
	return nil
}

func shortFrameError(typ byte) error {
	switch typ {
	case frameTypeAck:
		return ErrAckTooShort
	case frameTypeData:
		return ErrDataTooShort
	default:
		return ErrUnexpectedFrameType
	}
}

func decodeAckBody(frame transportFrame, data []byte) (transportFrame, error) {
	if len(data) < frameAckLen {
		return transportFrame{}, ErrAckTooShort
	}
	frame.seq = binary.BigEndian.Uint32(data[frameSeqOff:frameCRCOff])
	frame.crc = binary.BigEndian.Uint32(data[frameCRCOff:frameAckLen])
	return frame, nil
}

func decodeDataBody(frame transportFrame, data []byte) (transportFrame, error) {
	if len(data) < frameDataHdrLen {
		return transportFrame{}, ErrDataTooShort
	}
	frame.seq = binary.BigEndian.Uint32(data[frameSeqOff:frameCRCOff])
	frame.crc = binary.BigEndian.Uint32(data[frameCRCOff:frameAckLen])
	frame.totalLen = binary.BigEndian.Uint32(data[frameTotalLenOff:frameFragIdxOff])
	frame.fragIdx = binary.BigEndian.Uint16(data[frameFragIdxOff:frameFragTotalOff])
	frame.fragTotal = binary.BigEndian.Uint16(data[frameFragTotalOff:frameDataHdrLen])
	frame.payload = append([]byte(nil), data[frameDataHdrLen:]...)
	return frame, nil
}
