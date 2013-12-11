package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

type Server struct {
	Config
	Handshake func(*Config, *http.Request) error
	Handler
}

func (s Server) serveWebSocket(w http.ResponseWriter, req *http.Request) {
	rwc, buf, err := w.(http.Hijacker).Hijack()
	if err != nil {
		panic("Hijack failed: " + err.Error())
		return
	}
	defer rwc.Close()
	conn, err := newServerConn(rwc, buf, req, &s.Config, s.Handshake)
	if err != nil {
		return
	}
	if conn == nil {
		panic("unexpected nil conn")
	}
	s.Handler(conn)
}

func newServerConn(rwc io.ReadWriteCloser, buf *bufio.ReadWriter, req *http.Request, config *Config, handshake func(*Config, *http.Request) error) (conn *Conn, err error) {
	var hs serverHandshaker = &hybiServerHandshaker{Config: config}
	code, err := hs.ReadHandshake(buf.Reader, req)
	if err == ErrBadWebSocketVersion {
		fmt.Fprintf(buf, "HTTP/1.1 %03d %s\r\n", code, http.StatusText(code))
		fmt.Fprintf(buf, "Sec-WebSocket-Version: %s\r\n", SupportedProtocolVersion)
		buf.WriteString("\r\n")
		buf.WriteString(err.Error())
		buf.Flush()
		return
	}
	if err != nil {
		fmt.Fprintf(buf, "HTTP/1.1 %03d %s\r\n", code, http.StatusText(code))
		buf.WriteString("\r\n")
		buf.WriteString(err.Error())
		buf.Flush()
		return
	}
	if handshake != nil {
		err = handshake(config, req)
		if err != nil {
			code = http.StatusForbidden
			fmt.Fprintf(buf, "HTTP/1.1 %03d %s\r\n", code, http.StatusText(code))
			buf.WriteString("\r\n")
			buf.Flush()
			return
		}
	}
	err = hs.AcceptHandshake(buf.Writer)
	if err != nil {
		code = http.StatusBadRequest
		fmt.Fprintf(buf, "HTTP/1.1 %03d %s\r\n", code, http.StatusText(code))
		buf.WriteString("\r\n")
		buf.Flush()
		return
	}
	conn = hs.NewServerConn(buf, rwc, req)
	return
}

func (c *hybiServerHandshaker) AcceptHandshake(buf *bufio.Writer) (err error) {
	if len(c.Protocol) > 0 {
		if len(c.Protocol) != 1 {
			// You need choose a Protocol in Handshake func in Server.
			return ErrBadWebSocketProtocol
		}
	}
	buf.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	buf.WriteString("Upgrade: websocket\r\n")
	buf.WriteString("Connection: Upgrade\r\n")
	buf.WriteString("Sec-WebSocket-Accept: " + string(c.accept) + "\r\n")
	if len(c.Protocol) > 0 {
		buf.WriteString("Sec-WebSocket-Protocol: " + c.Protocol[0] + "\r\n")
	}
	// TODO(ukai): send Sec-WebSocket-Extensions.
	if c.Header != nil {
		err := c.Header.WriteSubset(buf, handshakeHeader)
		if err != nil {
			return err
		}
	}
	buf.WriteString("\r\n")
	return buf.Flush()
}

type hybiServerHandshaker struct {
	*Config
	accept []byte
}

type serverHandshaker interface {
	ReadHandshake(buf *bufio.Reader, req *http.Request) (code int, err error)
	AcceptHandshake(buf *bufio.Writer) (err error)
	NewServerConn(buf *bufio.ReadWriter, rwc io.ReadWriteCloser, request *http.Request) (conn *Conn)
}

func (c *hybiServerHandshaker) NewServerConn(buf *bufio.ReadWriter, rwc io.ReadWriteCloser, request *http.Request) *Conn {
	return newHybiServerConn(c.Config, buf, rwc, request)
}

func newHybiServerConn(config *Config, buf *bufio.ReadWriter, rwc io.ReadWriteCloser, request *http.Request) *Conn {
	return newHybiConn(config, buf, rwc, request)
}

func newHybiConn(config *Config, buf *bufio.ReadWriter, rwc io.ReadWriteCloser, request *http.Request) *Conn {
	if buf == nil {
		br := bufio.NewReader(rwc)
		bw := bufio.NewWriter(rwc)
		buf = bufio.NewReadWriter(br, bw)
	}
	ws := &Conn{config: config, request: request, buf: buf, rwc: rwc,
		frameReaderFactory: hybiFrameReaderFactory{buf.Reader},
		frameWriterFactory: hybiFrameWriterFactory{
			buf.Writer, request == nil},
		PayloadType:        TextFrame,
		defaultCloseStatus: closeStatusNormal}
	ws.frameHandler = &hybiFrameHandler{conn: ws}
	return ws
}

type hybiFrameWriterFactory struct {
	*bufio.Writer
	needMaskingKey bool
}

func (buf hybiFrameWriterFactory) NewFrameWriter(payloadType byte) (frame frameWriter, err error) {
	frameHeader := &hybiFrameHeader{Fin: true, OpCode: payloadType}
	if buf.needMaskingKey {
		frameHeader.MaskingKey, err = generateMaskingKey()
		if err != nil {
			return nil, err
		}
	}
	return &hybiFrameWriter{writer: buf.Writer, header: frameHeader}, nil
}

func (frame *hybiFrameWriter) Write(msg []byte) (n int, err error) {
	var header []byte
	var b byte
	if frame.header.Fin {
		b |= 0x80
	}
	for i := 0; i < 3; i++ {
		if frame.header.Rsv[i] {
			j := uint(6 - i)
			b |= 1 << j
		}
	}
	b |= frame.header.OpCode
	header = append(header, b)
	if frame.header.MaskingKey != nil {
		b = 0x80
	} else {
		b = 0
	}
	lengthFields := 0
	length := len(msg)
	switch {
	case length <= 125:
		b |= byte(length)
	case length < 65536:
		b |= 126
		lengthFields = 2
	default:
		b |= 127
		lengthFields = 8
	}
	header = append(header, b)
	for i := 0; i < lengthFields; i++ {
		j := uint((lengthFields - i - 1) * 8)
		b = byte((length >> j) & 0xff)
		header = append(header, b)
	}
	if frame.header.MaskingKey != nil {
		if len(frame.header.MaskingKey) != 4 {
			return 0, ErrBadMaskingKey
		}
		header = append(header, frame.header.MaskingKey...)
		frame.writer.Write(header)
		data := make([]byte, length)
		for i := range data {
			data[i] = msg[i] ^ frame.header.MaskingKey[i%4]
		}
		frame.writer.Write(data)
		err = frame.writer.Flush()
		return length, err
	}
	frame.writer.Write(header)
	frame.writer.Write(msg)
	err = frame.writer.Flush()
	return length, err
}

func (frame *hybiFrameWriter) Close() error { return nil }

type hybiFrameWriter struct {
	writer *bufio.Writer

	header *hybiFrameHeader
}

func generateMaskingKey() (maskingKey []byte, err error) {
	maskingKey = make([]byte, 4)
	if _, err = io.ReadFull(rand.Reader, maskingKey); err != nil {
		return
	}
	return
}
func (frame *hybiFrameReader) Len() (n int) { return frame.length }

func (frame *hybiFrameReader) TrailerReader() io.Reader { return nil }

func (frame *hybiFrameReader) Read(msg []byte) (n int, err error) {
	n, err = frame.reader.Read(msg)
	if err != nil {
		return 0, err
	}
	if frame.header.MaskingKey != nil {
		for i := 0; i < n; i++ {
			msg[i] = msg[i] ^ frame.header.MaskingKey[frame.pos%4]
			frame.pos++
		}
	}
	return n, err
}
func (buf hybiFrameReaderFactory) NewFrameReader() (frame frameReader, err error) {
	hybiFrame := new(hybiFrameReader)
	frame = hybiFrame
	var header []byte
	var b byte
	// First byte. FIN/RSV1/RSV2/RSV3/OpCode(4bits)
	b, err = buf.ReadByte()
	if err != nil {
		return
	}
	header = append(header, b)
	hybiFrame.header.Fin = ((header[0] >> 7) & 1) != 0
	for i := 0; i < 3; i++ {
		j := uint(6 - i)
		hybiFrame.header.Rsv[i] = ((header[0] >> j) & 1) != 0
	}
	hybiFrame.header.OpCode = header[0] & 0x0f

	// Second byte. Mask/Payload len(7bits)
	b, err = buf.ReadByte()
	if err != nil {
		return
	}
	header = append(header, b)
	mask := (b & 0x80) != 0
	b &= 0x7f
	lengthFields := 0
	switch {
	case b <= 125: // Payload length 7bits.
		hybiFrame.header.Length = int64(b)
	case b == 126: // Payload length 7+16bits
		lengthFields = 2
	case b == 127: // Payload length 7+64bits
		lengthFields = 8
	}
	for i := 0; i < lengthFields; i++ {
		b, err = buf.ReadByte()
		if err != nil {
			return
		}
		header = append(header, b)
		hybiFrame.header.Length = hybiFrame.header.Length*256 + int64(b)
	}
	if mask {
		// Masking key. 4 bytes.
		for i := 0; i < 4; i++ {
			b, err = buf.ReadByte()
			if err != nil {
				return
			}
			header = append(header, b)
			hybiFrame.header.MaskingKey = append(hybiFrame.header.MaskingKey, b)
		}
	}
	hybiFrame.reader = io.LimitReader(buf.Reader, hybiFrame.header.Length)
	hybiFrame.header.data = bytes.NewBuffer(header)
	hybiFrame.length = len(header) + int(hybiFrame.header.Length)
	return
}

type hybiFrameHandler struct {
	conn        *Conn
	payloadType byte
}

type hybiFrameHeader struct {
	Fin        bool
	Rsv        [3]bool
	OpCode     byte
	Length     int64
	MaskingKey []byte

	data *bytes.Buffer
}

type hybiFrameReader struct {
	reader io.Reader

	header hybiFrameHeader
	pos    int64
	length int
}

func (frame *hybiFrameReader) HeaderReader() io.Reader {
	if frame.header.data == nil {
		return nil
	}
	if frame.header.data.Len() == 0 {
		return nil
	}
	return frame.header.data
}

func (frame *hybiFrameReader) PayloadType() byte { return frame.header.OpCode }
func (ws *Conn) IsServerConn() bool              { return ws.request != nil }
func (handler *hybiFrameHandler) HandleFrame(frame frameReader) (r frameReader, err error) {
	if handler.conn.IsServerConn() {
		// The client MUST mask all frames sent to the server.
		if frame.(*hybiFrameReader).header.MaskingKey == nil {
			handler.WriteClose(closeStatusProtocolError)
			return nil, io.EOF
		}
	} else {
		// The server MUST NOT mask all frames.
		if frame.(*hybiFrameReader).header.MaskingKey != nil {
			handler.WriteClose(closeStatusProtocolError)
			return nil, io.EOF
		}
	}
	if header := frame.HeaderReader(); header != nil {
		io.Copy(ioutil.Discard, header)
	}
	switch frame.PayloadType() {
	case ContinuationFrame:
		frame.(*hybiFrameReader).header.OpCode = handler.payloadType
	case TextFrame, BinaryFrame:
		handler.payloadType = frame.PayloadType()
	case CloseFrame:
		return nil, io.EOF
	case PingFrame:
		pingMsg := make([]byte, maxControlFramePayloadLength)
		n, err := io.ReadFull(frame, pingMsg)
		if err != nil && err != io.ErrUnexpectedEOF {
			return nil, err
		}
		io.Copy(ioutil.Discard, frame)
		n, err = handler.WritePong(pingMsg[:n])
		if err != nil {
			return nil, err
		}
		return nil, nil
	case PongFrame:
		return nil, ErrNotImplemented
	}
	return frame, nil
}

func (handler *hybiFrameHandler) WriteClose(status int) (err error) {
	handler.conn.wio.Lock()
	defer handler.conn.wio.Unlock()
	w, err := handler.conn.frameWriterFactory.NewFrameWriter(CloseFrame)
	if err != nil {
		return err
	}
	msg := make([]byte, 2)
	binary.BigEndian.PutUint16(msg, uint16(status))
	_, err = w.Write(msg)
	w.Close()
	return err
}

func (handler *hybiFrameHandler) WritePong(msg []byte) (n int, err error) {
	handler.conn.wio.Lock()
	defer handler.conn.wio.Unlock()
	w, err := handler.conn.frameWriterFactory.NewFrameWriter(PongFrame)
	if err != nil {
		return 0, err
	}
	n, err = w.Write(msg)
	w.Close()
	return n, err
}

type hybiFrameReaderFactory struct {
	*bufio.Reader
}

func (c *hybiServerHandshaker) ReadHandshake(buf *bufio.Reader, req *http.Request) (code int, err error) {
	c.Version = ProtocolVersionHybi13
	if req.Method != "GET" {
		return http.StatusMethodNotAllowed, ErrBadRequestMethod
	}
	// HTTP version can be safely ignored.

	if strings.ToLower(req.Header.Get("Upgrade")) != "websocket" ||
		!strings.Contains(strings.ToLower(req.Header.Get("Connection")), "upgrade") {
		return http.StatusBadRequest, ErrNotWebSocket
	}

	key := req.Header.Get("Sec-Websocket-Key")
	if key == "" {
		return http.StatusBadRequest, ErrChallengeResponse
	}
	version := req.Header.Get("Sec-Websocket-Version")
	switch version {
	case "13":
		c.Version = ProtocolVersionHybi13
	default:
		return http.StatusBadRequest, ErrBadWebSocketVersion
	}
	var scheme string
	if req.TLS != nil {
		scheme = "wss"
	} else {
		scheme = "ws"
	}
	c.Location, err = url.ParseRequestURI(scheme + "://" + req.Host + req.URL.RequestURI())
	if err != nil {
		return http.StatusBadRequest, err
	}
	protocol := strings.TrimSpace(req.Header.Get("Sec-Websocket-Protocol"))
	if protocol != "" {
		protocols := strings.Split(protocol, ",")
		for i := 0; i < len(protocols); i++ {
			c.Protocol = append(c.Protocol, strings.TrimSpace(protocols[i]))
		}
	}
	c.accept, err = getNonceAccept([]byte(key))
	if err != nil {
		return http.StatusInternalServerError, err
	}
	return http.StatusSwitchingProtocols, nil
}

func getNonceAccept(nonce []byte) (expected []byte, err error) {
	h := sha1.New()
	if _, err = h.Write(nonce); err != nil {
		return
	}
	if _, err = h.Write([]byte(websocketGUID)); err != nil {
		return
	}
	expected = make([]byte, 28)
	base64.StdEncoding.Encode(expected, h.Sum(nil))
	return
}
