package main

import (
	"fmt"
	"net/http"
	"net/url"
)

type Config struct {
	Location *url.URL
	Origin   *url.URL
	Protocol []string
	Version  int
	//TlsConfig *tls.Config
	Header http.Header
	//handshakeData map[string]string
}

func Origin(config *Config, req *http.Request) (*url.URL, error) {
	var origin string
	switch config.Version {
	case ProtocolVersionHybi13:
		origin = req.Header.Get("Origin")
	}
	if origin == "null" {
		return nil, nil
	}
	return url.ParseRequestURI(origin)
}

func checkOrigin(config *Config, req *http.Request) (err error) {
	config.Origin, err = Origin(config, req)
	if err == nil && config.Origin == nil {
		return fmt.Errorf("null origin")
	}
	return err
}
