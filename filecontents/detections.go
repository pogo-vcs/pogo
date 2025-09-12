package filecontents

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/pogo-vcs/pogo/compressions"
)

type (
	FileType struct {
		Binary     bool
		Encoding   Encoding
		BOM        bool
		LineEnding LineEnding
	}

	Encoding   uint8
	LineEnding string
)

func (f FileType) String() string {
	return fmt.Sprintf(
		"Binary: %t, Encoding: %s, BOM: %t, LineEnding: %s",
		f.Binary,
		f.Encoding,
		f.BOM,
		f.LineEnding,
	)
}

const (
	UnknownEncoding Encoding = iota
	UTF8
	UTF16LE
	UTF16BE
	UTF32LE
	UTF32BE
)

func (e Encoding) String() string {
	switch e {
	case UTF8:
		return "UTF-8"
	case UTF16LE:
		return "UTF-16LE"
	case UTF16BE:
		return "UTF-16BE"
	case UTF32LE:
		return "UTF-32LE"
	case UTF32BE:
		return "UTF-32BE"
	default:
		return "unknown"
	}
}

const (
	UnknownLineEnding LineEnding = "unknown"
	LF                LineEnding = "\n"
	CRLF              LineEnding = "\r\n"
)

func (l LineEnding) String() string {
	switch l {
	case LF:
		return "LF"
	case CRLF:
		return "CRLF"
	default:
		return "unknown"
	}
}

const (
	Byte = 1
	KiB  = 1024 * Byte
	MiB  = 1024 * KiB
	GiB  = 1024 * MiB
)

func DetectFileType(fileName string) (FileType, error) {
	// check for file size (too large files are probably not text)
	stat, err := os.Stat(fileName)
	if err != nil {
		return FileType{}, errors.Join(fmt.Errorf("stat file %s", fileName), err)
	}
	size := stat.Size()
	if size > 1*GiB {
		return FileType{}, errors.New("file too large")
	} else if size == 0 {
		return FileType{
			Binary:     false,
			Encoding:   UnknownEncoding,
			LineEnding: UnknownLineEnding,
		}, nil
	}

	// read part of the file for content analysis
	bufferSize := 8 * KiB
	if size < int64(bufferSize) {
		bufferSize = int(size)
	}
	buffer := make([]byte, bufferSize)
	f, err := os.Open(fileName)
	if err != nil {
		return FileType{}, errors.Join(fmt.Errorf("open file %s", fileName), err)
	}
	defer f.Close()
	n, err := f.Read(buffer)
	if err != nil && err != io.EOF {
		return FileType{}, errors.Join(fmt.Errorf("read file %s", fileName), err)
	}
	if n == 0 {
		return FileType{}, errors.New("file is empty")
	}
	buffer = buffer[:n]

	ft := FileType{}

	// Check BOM first
	originalBuffer := buffer
	if bomEncoding := detectBOM(buffer); bomEncoding != UnknownEncoding {
		ft.Encoding = bomEncoding
		ft.BOM = true
		// Skip BOM for further analysis
		switch bomEncoding {
		case UTF8:
			buffer = buffer[3:]
		case UTF16LE, UTF16BE:
			buffer = buffer[2:]
		case UTF32LE, UTF32BE:
			buffer = buffer[4:]
		}
	}

	// Detect encoding if not already detected via BOM
	if ft.Encoding == UnknownEncoding {
		ft.Encoding = detectEncoding(buffer)
	}

	// Now check if binary based on the detected encoding
	if isBinary(buffer, ft.Encoding) {
		ft.Binary = true
		return ft, nil
	}

	// Detect line ending - use original buffer for UTF-16/32
	if ft.Encoding == UTF16LE || ft.Encoding == UTF16BE ||
		ft.Encoding == UTF32LE || ft.Encoding == UTF32BE {
		ft.LineEnding = detectLineEndingMultibyte(originalBuffer, ft.Encoding)
	} else {
		ft.LineEnding = detectLineEnding(buffer)
	}

	return ft, nil
}

func detectBOM(buffer []byte) Encoding {
	if len(buffer) >= 4 && buffer[0] == 0x00 && buffer[1] == 0x00 &&
		buffer[2] == 0xFE && buffer[3] == 0xFF {
		return UTF32BE
	}
	if len(buffer) >= 4 && buffer[0] == 0xFF && buffer[1] == 0xFE &&
		buffer[2] == 0x00 && buffer[3] == 0x00 {
		return UTF32LE
	}
	if len(buffer) >= 3 && buffer[0] == 0xEF && buffer[1] == 0xBB &&
		buffer[2] == 0xBF {
		return UTF8
	}
	if len(buffer) >= 2 && buffer[0] == 0xFE && buffer[1] == 0xFF {
		return UTF16BE
	}
	if len(buffer) >= 2 && buffer[0] == 0xFF && buffer[1] == 0xFE {
		return UTF16LE
	}
	return UnknownEncoding
}

func isBinary(buffer []byte, encoding Encoding) bool {
	if len(buffer) == 0 {
		return false
	}

	// For multibyte encodings, decode first
	var checkBuffer []byte
	switch encoding {
	case UTF16LE, UTF16BE, UTF32LE, UTF32BE:
		// Decode to UTF-8 for checking
		decoded := decodeToUTF8(buffer, encoding)
		if decoded == nil {
			return true // Failed to decode, likely binary
		}
		checkBuffer = decoded
	default:
		checkBuffer = buffer
	}

	nullBytes := 0
	controlChars := 0
	printableChars := 0
	highBytes := 0

	for _, b := range checkBuffer {
		if b == 0 {
			nullBytes++
		} else if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			controlChars++
		} else if b >= 0x20 && b < 0x7F {
			printableChars++
		} else if b >= 0x80 {
			highBytes++
		}
	}

	total := len(checkBuffer)

	// If any null bytes in decoded text, it's binary
	if nullBytes > 0 {
		return true
	}

	// If more than 30% control characters, it's binary
	if float64(controlChars)/float64(total) > 0.3 {
		return true
	}

	// If very few printable characters, it's likely binary
	if float64(printableChars)/float64(total) < 0.3 {
		return true
	}

	// For UTF-8, check if it's valid
	if encoding == UTF8 || encoding == UnknownEncoding {
		if !utf8.Valid(checkBuffer) {
			return true
		}
	}

	return false
}

func decodeToUTF8(buffer []byte, encoding Encoding) []byte {
	switch encoding {
	case UTF16LE:
		if len(buffer)%2 != 0 {
			buffer = buffer[:len(buffer)-1]
		}
		runes := make([]uint16, len(buffer)/2)
		for i := 0; i < len(buffer); i += 2 {
			runes[i/2] = uint16(buffer[i]) | uint16(buffer[i+1])<<8
		}
		return []byte(string(utf16.Decode(runes)))
	case UTF16BE:
		if len(buffer)%2 != 0 {
			buffer = buffer[:len(buffer)-1]
		}
		runes := make([]uint16, len(buffer)/2)
		for i := 0; i < len(buffer); i += 2 {
			runes[i/2] = uint16(buffer[i])<<8 | uint16(buffer[i+1])
		}
		return []byte(string(utf16.Decode(runes)))
	case UTF32LE:
		if len(buffer)%4 != 0 {
			buffer = buffer[:len(buffer)-(len(buffer)%4)]
		}
		var buf bytes.Buffer
		for i := 0; i < len(buffer); i += 4 {
			r := rune(uint32(buffer[i]) | uint32(buffer[i+1])<<8 |
				uint32(buffer[i+2])<<16 | uint32(buffer[i+3])<<24)
			if utf8.ValidRune(r) {
				buf.WriteRune(r)
			}
		}
		return buf.Bytes()
	case UTF32BE:
		if len(buffer)%4 != 0 {
			buffer = buffer[:len(buffer)-(len(buffer)%4)]
		}
		var buf bytes.Buffer
		for i := 0; i < len(buffer); i += 4 {
			r := rune(uint32(buffer[i])<<24 | uint32(buffer[i+1])<<16 |
				uint32(buffer[i+2])<<8 | uint32(buffer[i+3]))
			if utf8.ValidRune(r) {
				buf.WriteRune(r)
			}
		}
		return buf.Bytes()
	default:
		return buffer
	}
}

func detectEncoding(buffer []byte) Encoding {
	if len(buffer) == 0 {
		return UnknownEncoding
	}

	// Check for UTF-32 patterns
	if len(buffer) >= 4 {
		// UTF-32LE: every 4th byte starting from 3 is often 0
		// UTF-32BE: every 4th byte starting from 0 is often 0
		utf32leZeros := 0
		utf32beZeros := 0
		for i := 0; i < len(buffer)-3; i += 4 {
			if buffer[i] == 0 && buffer[i+1] == 0 && buffer[i+2] == 0 {
				utf32beZeros++
			}
			if buffer[i+1] == 0 && buffer[i+2] == 0 && buffer[i+3] == 0 {
				utf32leZeros++
			}
		}
		samples := len(buffer) / 4
		if samples > 0 {
			if float64(utf32beZeros)/float64(samples) > 0.5 {
				return UTF32BE
			}
			if float64(utf32leZeros)/float64(samples) > 0.5 {
				return UTF32LE
			}
		}
	}

	// Check for UTF-16 patterns
	if len(buffer) >= 2 {
		// Count null bytes in even and odd positions
		evenNulls := 0
		oddNulls := 0
		for i := 0; i < len(buffer)-1; i += 2 {
			if buffer[i] == 0 {
				evenNulls++
			}
			if buffer[i+1] == 0 {
				oddNulls++
			}
		}
		samples := len(buffer) / 2

		// UTF-16LE: ASCII chars have null in odd positions
		// UTF-16BE: ASCII chars have null in even positions
		if samples > 0 {
			if float64(oddNulls)/float64(samples) > 0.3 &&
				float64(evenNulls)/float64(samples) < 0.1 {
				return UTF16LE
			}
			if float64(evenNulls)/float64(samples) > 0.3 &&
				float64(oddNulls)/float64(samples) < 0.1 {
				return UTF16BE
			}
		}
	}

	// Check if valid UTF-8
	if utf8.Valid(buffer) {
		return UTF8
	}

	return UnknownEncoding
}

func detectLineEnding(buffer []byte) LineEnding {
	if len(buffer) == 0 {
		return UnknownLineEnding
	}

	var crlfCount, lfCount int

	for i := 0; i < len(buffer); i++ {
		switch buffer[i] {
		case '\r':
			// Check if it's part of CRLF
			if i+1 < len(buffer) && buffer[i+1] == '\n' {
				crlfCount++
				i++ // Skip the LF part of CRLF
			}
		case '\n':
			lfCount++
		}
	}

	// Determine the most common line ending
	maxCount := 0
	result := UnknownLineEnding

	if crlfCount > maxCount {
		maxCount = crlfCount
		result = CRLF
	}
	if lfCount > maxCount {
		maxCount = lfCount
		result = LF
	}

	return result
}

func detectLineEndingMultibyte(buffer []byte, encoding Encoding) LineEnding {
	// Decode to UTF-8 first
	decoded := decodeToUTF8(buffer, encoding)
	if decoded == nil {
		return UnknownLineEnding
	}
	return detectLineEnding(decoded)
}

// Canonicalizer implementations

type crlfCanonicalizer struct {
	r      io.Reader
	buffer []byte
	pos    int
	err    error
}

func (c *crlfCanonicalizer) Read(p []byte) (int, error) {
	if c.err != nil && c.pos >= len(c.buffer) {
		return 0, c.err
	}

	n := 0
	for n < len(p) {
		// Fill buffer if empty
		if c.pos >= len(c.buffer) && c.err == nil {
			c.buffer = make([]byte, 8192)
			nr, err := c.r.Read(c.buffer)
			c.buffer = c.buffer[:nr]
			c.pos = 0
			c.err = err
			if nr == 0 && err != nil {
				if n > 0 {
					return n, nil
				}
				return 0, err
			}
		}

		if c.pos >= len(c.buffer) {
			break
		}

		// Process buffer
		for c.pos < len(c.buffer) && n < len(p) {
			if c.buffer[c.pos] == '\r' && c.pos+1 < len(c.buffer) &&
				c.buffer[c.pos+1] == '\n' {
				p[n] = '\n'
				n++
				c.pos += 2
			} else if c.buffer[c.pos] == '\r' && c.pos+1 == len(c.buffer) &&
				c.err == nil {
				// Need to check next buffer for possible LF
				break
			} else {
				p[n] = c.buffer[c.pos]
				n++
				c.pos++
			}
		}
	}

	if n > 0 {
		return n, nil
	}
	return 0, c.err
}

type utf16leCanonicalizer struct {
	r      io.Reader
	buffer []byte
	outbuf []byte
	pos    int
}

func (c *utf16leCanonicalizer) Read(p []byte) (int, error) {
	// Copy any remaining output
	if c.pos < len(c.outbuf) {
		n := copy(p, c.outbuf[c.pos:])
		c.pos += n
		return n, nil
	}

	// Read more UTF-16LE data
	if c.buffer == nil {
		c.buffer = make([]byte, 8192)
	}
	n, err := c.r.Read(c.buffer)
	if n == 0 {
		return 0, err
	}

	// Decode UTF-16LE to UTF-8
	data := c.buffer[:n]
	if n%2 != 0 {
		// Handle odd byte at end
		data = data[:n-1]
	}

	runes := make([]uint16, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		runes[i/2] = uint16(data[i]) | uint16(data[i+1])<<8
	}

	decoded := string(utf16.Decode(runes))
	c.outbuf = []byte(decoded)
	c.pos = 0

	// Copy to output
	copied := copy(p, c.outbuf)
	c.pos = copied

	if copied == 0 && err == nil {
		// This should not happen with valid UTF-16, but handle gracefully
		return 0, io.ErrNoProgress
	}

	return copied, nil
}

type utf16beCanonicalizer struct {
	r      io.Reader
	buffer []byte
	outbuf []byte
	pos    int
}

func (c *utf16beCanonicalizer) Read(p []byte) (int, error) {
	// Copy any remaining output
	if c.pos < len(c.outbuf) {
		n := copy(p, c.outbuf[c.pos:])
		c.pos += n
		return n, nil
	}

	// Read more UTF-16BE data
	if c.buffer == nil {
		c.buffer = make([]byte, 8192)
	}
	n, err := c.r.Read(c.buffer)
	if n == 0 {
		return 0, err
	}

	// Decode UTF-16BE to UTF-8
	data := c.buffer[:n]
	if n%2 != 0 {
		// Handle odd byte at end
		data = data[:n-1]
	}

	runes := make([]uint16, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		runes[i/2] = uint16(data[i])<<8 | uint16(data[i+1])
	}

	decoded := string(utf16.Decode(runes))
	c.outbuf = []byte(decoded)
	c.pos = 0

	// Copy to output
	copied := copy(p, c.outbuf)
	c.pos = copied

	if copied == 0 && err == nil {
		return 0, io.ErrNoProgress
	}

	return copied, nil
}

type utf32leCanonicalizer struct {
	r      io.Reader
	buffer []byte
	outbuf []byte
	pos    int
}

func (c *utf32leCanonicalizer) Read(p []byte) (int, error) {
	// Copy any remaining output
	if c.pos < len(c.outbuf) {
		n := copy(p, c.outbuf[c.pos:])
		c.pos += n
		return n, nil
	}

	// Read more UTF-32LE data
	if c.buffer == nil {
		c.buffer = make([]byte, 8192)
	}
	n, err := c.r.Read(c.buffer)
	if n == 0 {
		return 0, err
	}

	// Decode UTF-32LE to UTF-8
	data := c.buffer[:n]
	if n%4 != 0 {
		// Handle incomplete rune at end
		data = data[:n-(n%4)]
	}

	var buf bytes.Buffer
	for i := 0; i < len(data); i += 4 {
		r := rune(uint32(data[i]) | uint32(data[i+1])<<8 |
			uint32(data[i+2])<<16 | uint32(data[i+3])<<24)
		if utf8.ValidRune(r) {
			buf.WriteRune(r)
		}
	}

	c.outbuf = buf.Bytes()
	c.pos = 0

	// Copy to output
	copied := copy(p, c.outbuf)
	c.pos = copied

	if copied == 0 && err == nil {
		return 0, io.ErrNoProgress
	}

	return copied, nil
}

type utf32beCanonicalizer struct {
	r      io.Reader
	buffer []byte
	outbuf []byte
	pos    int
}

func (c *utf32beCanonicalizer) Read(p []byte) (int, error) {
	// Copy any remaining output
	if c.pos < len(c.outbuf) {
		n := copy(p, c.outbuf[c.pos:])
		c.pos += n
		return n, nil
	}

	// Read more UTF-32BE data
	if c.buffer == nil {
		c.buffer = make([]byte, 8192)
	}
	n, err := c.r.Read(c.buffer)
	if n == 0 {
		return 0, err
	}

	// Decode UTF-32BE to UTF-8
	data := c.buffer[:n]
	if n%4 != 0 {
		// Handle incomplete rune at end
		data = data[:n-(n%4)]
	}

	var buf bytes.Buffer
	for i := 0; i < len(data); i += 4 {
		r := rune(uint32(data[i])<<24 | uint32(data[i+1])<<16 |
			uint32(data[i+2])<<8 | uint32(data[i+3]))
		if utf8.ValidRune(r) {
			buf.WriteRune(r)
		}
	}

	c.outbuf = buf.Bytes()
	c.pos = 0

	// Copy to output
	copied := copy(p, c.outbuf)
	c.pos = copied

	if copied == 0 && err == nil {
		return 0, io.ErrNoProgress
	}

	return copied, nil
}

func (ft FileType) CanonicalizeReader(r io.Reader) io.Reader {
	if ft.Binary {
		return r
	}

	switch ft.Encoding {
	case UTF16LE:
		r = &utf16leCanonicalizer{r: r}
	case UTF16BE:
		r = &utf16beCanonicalizer{r: r}
	case UTF32LE:
		r = &utf32leCanonicalizer{r: r}
	case UTF32BE:
		r = &utf32beCanonicalizer{r: r}
	}

	switch ft.LineEnding {
	case CRLF:
		r = &crlfCanonicalizer{r: r}
	}

	return r
}

type lfToCrlfConverter struct {
	r      io.Reader
	buffer []byte
	pos    int
	err    error
}

func (c *lfToCrlfConverter) Read(p []byte) (int, error) {
	if c.err != nil && c.pos >= len(c.buffer) {
		return 0, c.err
	}

	n := 0
	for n < len(p) {
		// Fill buffer if empty
		if c.pos >= len(c.buffer) && c.err == nil {
			temp := make([]byte, 4096)
			nr, err := c.r.Read(temp)
			c.err = err

			// Convert LF to CRLF in a new buffer
			c.buffer = make([]byte, 0, nr*2)
			for i := 0; i < nr; i++ {
				if temp[i] == '\n' {
					c.buffer = append(c.buffer, '\r', '\n')
				} else {
					c.buffer = append(c.buffer, temp[i])
				}
			}
			c.pos = 0

			if len(c.buffer) == 0 && err != nil {
				if n > 0 {
					return n, nil
				}
				return 0, err
			}
		}

		if c.pos >= len(c.buffer) {
			break
		}

		// Copy from buffer to output
		copied := copy(p[n:], c.buffer[c.pos:])
		n += copied
		c.pos += copied
	}

	if n > 0 {
		return n, nil
	}
	return 0, c.err
}

type utf8ToUtf16leConverter struct {
	r      io.Reader
	buffer []byte
	outbuf []byte
	pos    int
}

func (c *utf8ToUtf16leConverter) Read(p []byte) (int, error) {
	// Copy any remaining output
	if c.pos < len(c.outbuf) {
		n := copy(p, c.outbuf[c.pos:])
		c.pos += n
		return n, nil
	}

	// Read more UTF-8 data
	if c.buffer == nil {
		c.buffer = make([]byte, 4096)
	}
	n, err := c.r.Read(c.buffer)
	if n == 0 {
		return 0, err
	}

	// Find valid UTF-8 boundary
	data := c.buffer[:n]
	validEnd := n
	for i := n - 1; i >= 0 && i >= n-4; i-- {
		if utf8.RuneStart(data[i]) {
			r, size := utf8.DecodeRune(data[i:])
			if r != utf8.RuneError && i+size <= n {
				validEnd = i + size
				break
			}
			validEnd = i
			break
		}
	}
	data = data[:validEnd]

	// Convert UTF-8 to UTF-16LE
	runes := []rune(string(data))
	utf16Data := utf16.Encode(runes)

	c.outbuf = make([]byte, len(utf16Data)*2)
	for i, u := range utf16Data {
		c.outbuf[i*2] = byte(u)
		c.outbuf[i*2+1] = byte(u >> 8)
	}
	c.pos = 0

	// Copy to output
	copied := copy(p, c.outbuf)
	c.pos = copied

	return copied, nil
}

type utf8ToUtf16beConverter struct {
	r      io.Reader
	buffer []byte
	outbuf []byte
	pos    int
}

func (c *utf8ToUtf16beConverter) Read(p []byte) (int, error) {
	// Copy any remaining output
	if c.pos < len(c.outbuf) {
		n := copy(p, c.outbuf[c.pos:])
		c.pos += n
		return n, nil
	}

	// Read more UTF-8 data
	if c.buffer == nil {
		c.buffer = make([]byte, 4096)
	}
	n, err := c.r.Read(c.buffer)
	if n == 0 {
		return 0, err
	}

	// Find valid UTF-8 boundary
	data := c.buffer[:n]
	validEnd := n
	for i := n - 1; i >= 0 && i >= n-4; i-- {
		if utf8.RuneStart(data[i]) {
			r, size := utf8.DecodeRune(data[i:])
			if r != utf8.RuneError && i+size <= n {
				validEnd = i + size
				break
			}
			validEnd = i
			break
		}
	}
	data = data[:validEnd]

	// Convert UTF-8 to UTF-16BE
	runes := []rune(string(data))
	utf16Data := utf16.Encode(runes)

	c.outbuf = make([]byte, len(utf16Data)*2)
	for i, u := range utf16Data {
		c.outbuf[i*2] = byte(u >> 8)
		c.outbuf[i*2+1] = byte(u)
	}
	c.pos = 0

	// Copy to output
	copied := copy(p, c.outbuf)
	c.pos = copied

	return copied, nil
}

type utf8ToUtf32leConverter struct {
	r      io.Reader
	buffer []byte
	outbuf []byte
	pos    int
}

func (c *utf8ToUtf32leConverter) Read(p []byte) (int, error) {
	// Copy any remaining output
	if c.pos < len(c.outbuf) {
		n := copy(p, c.outbuf[c.pos:])
		c.pos += n
		return n, nil
	}

	// Read more UTF-8 data
	if c.buffer == nil {
		c.buffer = make([]byte, 4096)
	}
	n, err := c.r.Read(c.buffer)
	if n == 0 {
		return 0, err
	}

	// Find valid UTF-8 boundary
	data := c.buffer[:n]
	validEnd := n
	for i := n - 1; i >= 0 && i >= n-4; i-- {
		if utf8.RuneStart(data[i]) {
			r, size := utf8.DecodeRune(data[i:])
			if r != utf8.RuneError && i+size <= n {
				validEnd = i + size
				break
			}
			validEnd = i
			break
		}
	}
	data = data[:validEnd]

	// Convert UTF-8 to UTF-32LE
	runes := []rune(string(data))

	c.outbuf = make([]byte, len(runes)*4)
	for i, r := range runes {
		c.outbuf[i*4] = byte(r)
		c.outbuf[i*4+1] = byte(r >> 8)
		c.outbuf[i*4+2] = byte(r >> 16)
		c.outbuf[i*4+3] = byte(r >> 24)
	}
	c.pos = 0

	// Copy to output
	copied := copy(p, c.outbuf)
	c.pos = copied

	return copied, nil
}

type utf8ToUtf32beConverter struct {
	r      io.Reader
	buffer []byte
	outbuf []byte
	pos    int
}

func (c *utf8ToUtf32beConverter) Read(p []byte) (int, error) {
	// Copy any remaining output
	if c.pos < len(c.outbuf) {
		n := copy(p, c.outbuf[c.pos:])
		c.pos += n
		return n, nil
	}

	// Read more UTF-8 data
	if c.buffer == nil {
		c.buffer = make([]byte, 4096)
	}
	n, err := c.r.Read(c.buffer)
	if n == 0 {
		return 0, err
	}

	// Find valid UTF-8 boundary
	data := c.buffer[:n]
	validEnd := n
	for i := n - 1; i >= 0 && i >= n-4; i-- {
		if utf8.RuneStart(data[i]) {
			r, size := utf8.DecodeRune(data[i:])
			if r != utf8.RuneError && i+size <= n {
				validEnd = i + size
				break
			}
			validEnd = i
			break
		}
	}
	data = data[:validEnd]

	// Convert UTF-8 to UTF-32BE
	runes := []rune(string(data))

	c.outbuf = make([]byte, len(runes)*4)
	for i, r := range runes {
		c.outbuf[i*4] = byte(r >> 24)
		c.outbuf[i*4+1] = byte(r >> 16)
		c.outbuf[i*4+2] = byte(r >> 8)
		c.outbuf[i*4+3] = byte(r)
	}
	c.pos = 0

	// Copy to output
	copied := copy(p, c.outbuf)
	c.pos = copied

	return copied, nil
}

func (ft FileType) TypeReader(r io.Reader) io.Reader {
	if ft.Binary {
		return r
	}

	// First convert line endings (reverse of canonicalize)
	switch ft.LineEnding {
	case CRLF:
		r = &lfToCrlfConverter{r: r}
	}

	// Then convert encoding (reverse of canonicalize)
	switch ft.Encoding {
	case UTF16LE:
		r = &utf8ToUtf16leConverter{r: r}
	case UTF16BE:
		r = &utf8ToUtf16beConverter{r: r}
	case UTF32LE:
		r = &utf8ToUtf32leConverter{r: r}
	case UTF32BE:
		r = &utf8ToUtf32beConverter{r: r}
	}

	return r
}

func ThreeWayMergeResultType(a, o, b FileType) FileType {
	t := FileType{
		o.Binary,
		o.Encoding,
		o.BOM,
		o.LineEnding,
	}

	if a.Binary != o.Binary || o.Binary != b.Binary {
		t.Binary = !o.Binary
	}

	if a.BOM != o.BOM || o.BOM != b.BOM {
		t.BOM = !o.BOM
	}

	if a.Encoding != o.Encoding {
		t.Encoding = a.Encoding
	} else if b.Encoding != o.Encoding {
		t.Encoding = b.Encoding
	} else {
		t.Encoding = o.Encoding
	}

	if a.LineEnding != o.LineEnding {
		t.LineEnding = a.LineEnding
	} else if b.LineEnding != o.LineEnding {
		t.LineEnding = b.LineEnding
	} else {
		t.LineEnding = o.LineEnding
	}

	return t
}

func HasConflictMarkers(filePath string) (bool, error) {
	// For object store files, we need to extract the hash and use the decompression path
	hashStr := filepath.Base(filepath.Dir(filePath)) + filepath.Base(filePath)
	if len(hashStr) >= 2 {
		// This looks like it might be an object store path, try using OpenFileByHashWithType
		reader, fileType, err := OpenFileByHashWithType(hashStr)
		if err == nil {
			defer reader.Close()

			if fileType.Binary {
				return false, nil
			}

			scanner := bytes.NewBuffer(nil)
			_, err = io.Copy(scanner, reader)
			if err != nil {
				return false, errors.Join(fmt.Errorf("read file %s", filePath), err)
			}

			content := scanner.Bytes()
			hasStart := bytes.Contains(content, []byte("<<<<<<<"))
			hasSeparator := bytes.Contains(content, []byte("======="))
			hasEnd := bytes.Contains(content, []byte(">>>>>>>"))

			return hasStart && hasSeparator && hasEnd, nil
		}
	}

	// Fallback to handle files directly (with potential compression)
	var reader io.ReadCloser
	var fileType FileType
	var err error
	var file *os.File

	// Check if file is compressed and handle accordingly
	if isZstdCompressed(filePath) {
		// For compressed files, create temporary decompressed file for type detection
		tempFile, err := createTempDecompressedFile(filePath)
		if err != nil {
			return false, errors.Join(fmt.Errorf("create temp decompressed file %s", filePath), err)
		}
		defer os.Remove(tempFile)

		fileType, err = DetectFileType(tempFile)
		if err != nil {
			return false, errors.Join(fmt.Errorf("detect file type %s", filePath), err)
		}

		// Open compressed file with decompression
		file, err = os.Open(filePath)
		if err != nil {
			return false, errors.Join(fmt.Errorf("open file %s", filePath), err)
		}
		reader = compressions.Decompress(file)
	} else {
		// For uncompressed files, handle normally
		fileType, err = DetectFileType(filePath)
		if err != nil {
			return false, errors.Join(fmt.Errorf("detect file type %s", filePath), err)
		}

		file, err = os.Open(filePath)
		if err != nil {
			return false, errors.Join(fmt.Errorf("open file %s", filePath), err)
		}
		reader = file
	}

	defer reader.Close()

	if fileType.Binary {
		return false, nil
	}

	scanner := bytes.NewBuffer(nil)
	_, err = io.Copy(scanner, reader)
	if err != nil {
		return false, errors.Join(fmt.Errorf("read file %s", filePath), err)
	}

	content := scanner.Bytes()

	hasStart := bytes.Contains(content, []byte("<<<<<<<"))
	hasSeparator := bytes.Contains(content, []byte("======="))
	hasEnd := bytes.Contains(content, []byte(">>>>>>>"))

	return hasStart && hasSeparator && hasEnd, nil
}

var binaryConflictFileNameRegex = regexp.MustCompile(`^.*\.conflict-(?:a|b)-[abcdefhkmnprwxyACDEFHJKLMNPRXY34]{16}$`)

func IsBinaryConflictFileName(filePath string) bool {
	c := binaryConflictFileNameRegex.MatchString(filePath)
	fmt.Printf("IsBinaryConflictFileName: %q %t\n", filePath, c)
	return c
}
