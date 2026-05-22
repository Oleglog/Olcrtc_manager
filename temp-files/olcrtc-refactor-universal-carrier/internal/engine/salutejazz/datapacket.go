package salutejazz

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/google/uuid"
)

func encodeVarint(value uint64) []byte {
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(buf, value)
	return buf[:n]
}

func encodeField(fieldNumber int, wireType int, data []byte) []byte {
	tag := encodeVarint(uint64(fieldNumber)<<3 | uint64(wireType)) //nolint:gosec,lll // G115: bounded conversion verified by surrounding logic
	switch wireType {
	case 2:
		length := encodeVarint(uint64(len(data)))
		result := make([]byte, 0, len(tag)+len(length)+len(data))
		result = append(result, tag...)
		result = append(result, length...)
		result = append(result, data...)
		return result
	default:
		result := make([]byte, 0, len(tag)+len(data))
		result = append(result, tag...)
		result = append(result, data...)
		return result
	}
}

// EncodeDataPacket wraps a payload into a SaluteJazz data packet.
func EncodeDataPacket(payload []byte) []byte {
	msgID := uuid.New().String()

	userFields := encodeField(2, 2, payload)
	userFields = append(userFields, encodeField(8, 2, []byte(msgID))...)

	dp := encodeField(1, 0, encodeVarint(0))
	dp = append(dp, encodeField(2, 2, userFields)...)

	return dp
}

func readVarint(r io.ByteReader) (uint64, error) {
	val, err := binary.ReadUvarint(r)
	if err != nil {
		return 0, fmt.Errorf("read uvarint: %w", err)
	}
	return val, nil
}

// DecodeDataPacket extracts the payload from a SaluteJazz data packet.
func DecodeDataPacket(raw []byte) ([]byte, bool) {
	userData, ok := parseFields(raw, 2)
	if !ok {
		return nil, false
	}

	payload, ok := parseFields(userData, 2)
	return payload, ok
}

func parseFields(data []byte, targetField int) ([]byte, bool) {
	reader := &byteReader{data: data, pos: 0}
	var result []byte

	for reader.pos < len(reader.data) {
		tagVal, err := readVarint(reader)
		if err != nil {
			break
		}

		fieldNumber := int(tagVal >> 3)
		wireType := int(tagVal & 0x07)

		fieldData, ok := handleWireType(reader, wireType, len(data))
		if !ok {
			return result, len(result) > 0
		}

		if fieldNumber == targetField && wireType == 2 {
			result = fieldData
		}
	}

	return result, len(result) > 0
}

func handleWireType(reader *byteReader, wireType int, dataLen int) ([]byte, bool) {
	switch wireType {
	case 0:
		_, _ = readVarint(reader)
		return nil, true
	case 2:
		length, err := readVarint(reader)
		if err != nil {
			return nil, false
		}
		if length > uint64(dataLen)-uint64(reader.pos) { //nolint:gosec,lll // G115: bounded conversion verified by surrounding logic
			return nil, false
		}
		fieldData := make([]byte, length)
		n, err := reader.Read(fieldData)
		if err != nil || uint64(n) != length { //nolint:gosec // G115: bounded conversion verified by surrounding logic
			return nil, false
		}
		return fieldData, true
	case 1:
		reader.pos += 8
		return nil, true
	case 5:
		reader.pos += 4
		return nil, true
	default:
		return nil, false
	}
}

type byteReader struct {
	data []byte
	pos  int
}

func (b *byteReader) ReadByte() (byte, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	c := b.data[b.pos]
	b.pos++
	return c, nil
}

func (b *byteReader) Read(p []byte) (int, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}
