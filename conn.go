package main

import (
	"bufio"
	"io"
	"io/ioutil"
	"net/http"
	"sync"
)

type Conn struct {
	config  *Config
	buf     *bufio.ReadWriter
	request *http.Request
	rwc     io.ReadWriteCloser
	frameHandler
	defaultCloseStatus int
	wio                sync.Mutex
	PayloadType        byte
	frameWriterFactory
	rio sync.Mutex
	frameReader
	frameReaderFactory
}

func (was *Conn) Close() error {
	err := was.frameHandler.WriteClose(was.defaultCloseStatus)
	if err != nil {
		return err
	}
	return was.rwc.Close()
}

func (was *Conn) Write(msg []byte) (n int, err error) {
	was.wio.Lock()
	defer was.wio.Unlock()
	w, err := was.frameWriterFactory.NewFrameWriter(was.PayloadType)
	if err != nil {
		return 0, err
	}
	n, err = w.Write(msg)
	w.Close()
	if err != nil {
		return n, err
	}
	return n, err
}

type frameWriterFactory interface {
	NewFrameWriter(payloadType byte) (w frameWriter, err error)
}

type frameWriter interface {
	// Writer is to write playload of the frame.
	io.WriteCloser
}

func (was *Conn) Read(msg []byte) (n int, err error) {
	was.rio.Lock()
	defer was.rio.Unlock()
again:
	if was.frameReader == nil {
		frame, err := was.frameReaderFactory.NewFrameReader()
		if err != nil {
			return 0, err
		}
		was.frameReader, err = was.frameHandler.HandleFrame(frame)
		if err != nil {
			return 0, err
		}
		if was.frameReader == nil {
			goto again
		}
	}
	n, err = was.frameReader.Read(msg)
	if err == io.EOF {
		if trailer := was.frameReader.TrailerReader(); trailer != nil {
			io.Copy(ioutil.Discard, trailer)
		}
		was.frameReader = nil
		goto again
	}
	return n, err
}

type frameReaderFactory interface {
	NewFrameReader() (r frameReader, err error)
}

type frameReader interface {
	io.Reader
	TrailerReader() io.Reader
	PayloadType() byte
	HeaderReader() io.Reader
	Len() int
}

type frameHandler interface {
	HandleFrame(frame frameReader) (r frameReader, err error)
	WriteClose(status int) (err error)
}
