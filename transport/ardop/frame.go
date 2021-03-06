// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package ardop

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"regexp"
)

type frame interface{}

type dFrame struct {
	dataType string
	data     []byte
}

func (f dFrame) ARQFrame() bool { return f.dataType == "ARQ" }
func (f dFrame) FECFrame() bool { return f.dataType == "FEC" }
func (f dFrame) ErrFrame() bool { return f.dataType == "ERR" }
func (f dFrame) IDFrame() bool  { return f.dataType == "IDF" }

type cmdFrame string

func (f cmdFrame) Parsed() ctrlMsg { return parseCtrlMsg(string(f)) }

func writeCtrlFrame(isTCP bool, w io.Writer, format string, params ...interface{}) error {
	var prefix string
	if !isTCP {
		prefix = "C:"
	}

	payload := fmt.Sprintf(format+"\r", params...)
	_, err := fmt.Fprint(w, prefix+payload)

	if !isTCP && err == nil {
		sum := crc16Sum([]byte(payload))
		err = binary.Write(w, binary.BigEndian, sum)
	}

	return err
}

func readFrameOfType(fType byte, reader *bufio.Reader, isTCP bool) (frame, error) {
	var err error
	var data []byte
	switch fType {
	case '*': // !isTCP
		fType, err = reader.ReadByte()
		if err != nil {
			return nil, err
		}
		reader.ReadByte() // Discard ';'. (TODO: Use reader.Discard(1) when we drop support for Go <= 1.4).
		return readFrameOfType(fType, reader, isTCP)
	case 'c':
		data, err = reader.ReadBytes('\r')
	case 'd':
		// Peek length
		peeked, err := reader.Peek(2)
		if err != nil {
			return nil, err
		}
		length := binary.BigEndian.Uint16(peeked) + 2 // +2 to include the length bytes

		// actual data
		data = make([]byte, length)
		var n int
		for read := 0; read < int(length) && err == nil; {
			n, err = reader.Read(data[read:])
			read += n
		}
	default:
		return nil, fmt.Errorf("Unexpected frame type %c", fType)
	}

	if err != nil {
		return nil, err
	}

	// Verify CRC sums
	if !isTCP {
		sumBytes := make([]byte, 2)
		reader.Read(sumBytes)
		crc := binary.BigEndian.Uint16(sumBytes)
		if crc16Sum(data) != crc {
			return nil, ErrChecksumMismatch
		}
	}

	switch fType {
	case 'c':
		data = data[:len(data)-1] // Trim \r
		return cmdFrame(string(data)), nil
	case 'd':
		return dFrame{dataType: string(data[2:5]), data: data[5:]}, nil
	default:
		panic("not possible")
	}
}

// Data example: " LA5NTA:[JP20QE] "
var reID = regexp.MustCompile(`(\w+)[:\s]*\[(\w+)\]`)

func parseIDFrame(df dFrame) (callsign, gridsquare string, err error) {
	if !df.IDFrame() {
		return "", "", errors.New("Unexpected frame type")
	}

	matches := reID.FindSubmatch(df.data)
	if len(matches) != 3 {
		return "", "", errors.New("Unexpected ID format")
	}

	return string(matches[1]), string(matches[2]), nil
}
