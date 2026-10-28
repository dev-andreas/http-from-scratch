package http1

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrMalformedRequestLine   = errors.New("malformed request line")
	ErrMalformedMethod        = errors.New("malformed method")
	ErrMalformedHTTPVersion   = errors.New("malformed HTTP version")
	ErrMalformedRequestURI    = errors.New("malformed request URI")
	ErrMalformedHeader        = errors.New("malformed header")
	ErrMalformedContentLength = errors.New("malformed content length")
	ErrInvalidLineBreak       = errors.New("invalid line break")
	ErrHeaderLineTooLong      = errors.New("header line too long")
	ErrTooManyHeaders         = errors.New("too many headers")
	ErrUnexpectedBodyEOF      = errors.New("unexpected EOF while reading body")
	ErrMalformedChunkSize     = errors.New("malformed chunk size")
	ErrChunkSizeMismatch      = errors.New("chunk size mismatch")

	MethodRegex      = regexp.MustCompile(`^[a-zA-Z0-9!#$%&'*+\-.^_` + "`" + `|~]+$`)
	ProtoRegex       = regexp.MustCompile(`^HTTP/(\d+)\.(\d+)$`)
	HeaderNameRegex  = regexp.MustCompile(`^[a-zA-Z0-9!#$&'*+\-.^_` + "`" + `|~]+$`)
	HeaderValueRegex = regexp.MustCompile(`^[\x20-\x7E\x80-\xFF]*$`)

	MaxRequestLineBytes = 8192
	MaxHeaders          = 100
)

type chunkedReader struct {
	reader    *bufio.Reader
	header    http.Header
	bytesLeft int64
	done      bool
}

func (cr *chunkedReader) Read(p []byte) (int, error) {
	if cr.done {
		return 0, io.EOF
	}

	if cr.bytesLeft == 0 {
		line, err := readLine(cr.reader)
		if err != nil {
			return 0, err
		}

		if idx := strings.IndexByte(line, ';'); idx != -1 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)

		size, err := strconv.ParseInt(line, 16, 64)
		if err != nil || size < 0 {
			return 0, fmt.Errorf("%w: %s", ErrMalformedChunkSize, line)
		}

		if size == 0 {
			cr.done = true
			if err := cr.readTrailers(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}

		cr.bytesLeft = size
	}

	readSize := min(int64(len(p)), cr.bytesLeft)

	n, err := cr.reader.Read(p[:readSize])
	cr.bytesLeft -= int64(n)

	if cr.bytesLeft == 0 && err == nil {
		crlf := make([]byte, 2)
		if _, readErr := io.ReadFull(cr.reader, crlf); readErr != nil {
			return n, fmt.Errorf("%w: missing CRLF after chunk", ErrInvalidLineBreak)
		}
		if crlf[0] != '\r' || crlf[1] != '\n' {
			return n, ErrInvalidLineBreak
		}
	}

	return n, err
}

func (cr *chunkedReader) Close() error {
	return nil
}

func (cr *chunkedReader) readTrailers() error {
	trailers, err := readHeader(cr.reader)
	if err != nil {
		return err
	}
	for k, v := range trailers {
		cr.header[k] = append(cr.header[k], v...)
	}
	return nil
}

func ParseRequest(reader *bufio.Reader) (*http.Request, error) {
	req := &http.Request{}

	if err := parseRequestLine(reader, req); err != nil {
		return nil, err
	}

	header, err := readHeader(reader)
	if err != nil {
		return nil, err
	}
	if len(header) > MaxHeaders {
		return nil, ErrTooManyHeaders
	}
	req.Header = header
	req.Host = header.Get("Host")

	body, err := readBody(reader, header)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}
	req.Body = body

	return req, nil
}

func parseRequestLine(reader *bufio.Reader, req *http.Request) error {
	line, err := readLine(reader)
	if err != nil {
		return err
	}

	parts := strings.Split(line, " ")
	if len(parts) < 3 {
		return fmt.Errorf("%w: %s", ErrMalformedRequestLine, line)
	}

	req.Method = strings.TrimSpace(parts[0])
	if !MethodRegex.MatchString(req.Method) {
		return fmt.Errorf("%w,%w: %s", ErrMalformedRequestLine, ErrMalformedMethod, req.Method)
	}

	parsedUrl, err := url.ParseRequestURI(parts[1])
	if err != nil {
		return fmt.Errorf("%w,%w: %s", ErrMalformedRequestLine, ErrMalformedRequestURI, parts[1])
	}
	req.URL = parsedUrl

	req.Proto = parts[2]
	matches := ProtoRegex.FindStringSubmatch(req.Proto)
	if matches == nil {
		return fmt.Errorf("%w,%w: %s", ErrMalformedRequestLine, ErrMalformedHTTPVersion, parts[2])
	}

	req.ProtoMajor, _ = strconv.Atoi(matches[1])
	req.ProtoMinor, _ = strconv.Atoi(matches[2])

	return nil
}

func readHeader(reader *bufio.Reader) (http.Header, error) {
	header := make(http.Header)
	for {
		line, err := readLine(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrMalformedHeader, line)
		}

		if line == "" {
			break
		}

		before, after, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMalformedHeader, line)
		}

		key := strings.TrimSpace(before)
		if !HeaderNameRegex.MatchString(key) {
			return nil, fmt.Errorf("%w: %s", ErrMalformedHeader, line)
		}
		value := strings.TrimSpace(after)
		if !HeaderValueRegex.MatchString(value) {
			return nil, fmt.Errorf("%w: %s", ErrMalformedHeader, line)
		}

		header.Add(key, value)
	}
	return header, nil
}

func readBody(reader *bufio.Reader, header http.Header) (io.ReadCloser, error) {
	if isChunked(header.Get("Transfer-Encoding")) {
		var bodyBuf bytes.Buffer

		for {
			line, err := readLine(reader)
			if err != nil {
				if errors.Is(err, ErrInvalidLineBreak) || errors.Is(err, io.EOF) {
					return nil, fmt.Errorf("%w: %w", ErrMalformedChunkSize, err)
				}
				return nil, err
			}

			chunkSizeStr := line
			if idx := strings.IndexByte(line, ';'); idx != -1 {
				chunkSizeStr = line[:idx]
			}
			chunkSizeStr = strings.TrimSpace(chunkSizeStr)

			chunkSize, err := strconv.ParseInt(chunkSizeStr, 16, 64)
			if err != nil || chunkSize < 0 {
				return nil, fmt.Errorf("%w: invalid hex size %q", ErrMalformedChunkSize, chunkSizeStr)
			}

			if chunkSize == 0 {
				if err := parseTrailers(reader, header); err != nil {
					return nil, err
				}
				break
			}

			chunkData := make([]byte, chunkSize)
			n, err := io.ReadFull(reader, chunkData)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrChunkSizeMismatch, chunkSize, n)
				}
				return nil, err
			}

			crlf := make([]byte, 2)
			if _, err := io.ReadFull(reader, crlf); err != nil || string(crlf) != "\r\n" {
				return nil, fmt.Errorf("%w: missing CRLF terminator after chunk data", ErrChunkSizeMismatch)
			}

			bodyBuf.Write(chunkData)
		}

		return io.NopCloser(&bodyBuf), nil
	}

	if contentLengthStr := header.Get("Content-Length"); contentLengthStr != "" {
		contentLength, err := strconv.ParseInt(strings.TrimSpace(contentLengthStr), 10, 64)
		if err != nil || contentLength < 0 {
			return nil, fmt.Errorf("%w: %s", ErrMalformedContentLength, contentLengthStr)
		}

		if contentLength > 0 {
			buf := make([]byte, contentLength)
			_, err := io.ReadFull(reader, buf)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					return nil, fmt.Errorf("%w: expected %d bytes", ErrUnexpectedBodyEOF, contentLength)
				}
				return nil, err
			}
			return io.NopCloser(bytes.NewReader(buf)), nil
		}
	}

	return http.NoBody, nil
}

func isChunked(teHeader string) bool {
	parts := strings.Split(teHeader, ",")
	for _, p := range parts {
		if strings.EqualFold(strings.TrimSpace(p), "chunked") {
			return true
		}
	}
	return false
}

func parseTrailers(reader *bufio.Reader, header http.Header) error {
	for {
		line, err := readLine(reader)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrMalformedHeader, err)
		}
		if line == "" {
			return nil
		}

		before, after, ok := strings.Cut(line, ":")
		if !ok {
			return fmt.Errorf("%w: %s", ErrMalformedHeader, line)
		}

		key := strings.TrimSpace(before)
		val := strings.TrimSpace(after)
		if HeaderNameRegex.MatchString(key) && HeaderValueRegex.MatchString(val) {
			header.Add(key, val)
		}
	}
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", ErrInvalidLineBreak
	}

	if len(line) > MaxRequestLineBytes {
		return "", ErrHeaderLineTooLong
	}

	if !strings.HasSuffix(line, "\r\n") {
		return "", ErrInvalidLineBreak
	}

	return line[:len(line)-2], nil
}
