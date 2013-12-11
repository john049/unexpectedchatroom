package main

import (
	"fmt"
	"net/http"
)

func getString(ch chan string, name string, data []byte) {
	ch <- fmt.Sprintf("%s\t> %s", name, data)
}

type Handler func(*Conn)

func (h Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s := Server{Handler: h, Handshake: checkOrigin}
	s.serveWebSocket(w, req) //here
}

type ProtocolError struct {
	ErrorString string
}

func (err *ProtocolError) Error() string { return err.ErrorString }

var (
	ErrBadWebSocketProtocol = &ProtocolError{"missing or bad WebSocket-Protocol"}
	ErrBadWebSocketVersion  = &ProtocolError{"missing or bad WebSocket Version"}
	ErrChallengeResponse    = &ProtocolError{"mismatch challenge/response"}
	ErrNotWebSocket         = &ProtocolError{"not websocket protocol"}
	ErrBadRequestMethod     = &ProtocolError{"bad method"}
)

var (
	ErrBadMaskingKey  = &ProtocolError{"bad masking key"}
	ErrNotImplemented = &ProtocolError{"not implemented"}
	handshakeHeader   = map[string]bool{
		"Host":                   true,
		"Upgrade":                true,
		"Connection":             true,
		"Sec-Websocket-Key":      true,
		"Sec-Websocket-Origin":   true,
		"Sec-Websocket-Version":  true,
		"Sec-Websocket-Protocol": true,
		"Sec-Websocket-Accept":   true,
	}
)

const (
	ProtocolVersionHybi13    = 13
	SupportedProtocolVersion = "13"
	TextFrame                = 1
	ContinuationFrame        = 0
	BinaryFrame              = 2
	CloseFrame               = 8
	PingFrame                = 9
	PongFrame                = 10
	//UnknownFrame      = 255
)

const (
	websocketGUID                = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	closeStatusNormal            = 1000
	closeStatusProtocolError     = 1002
	maxControlFramePayloadLength = 125
)
