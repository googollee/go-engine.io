package websocket

import (
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/shiyanlin/go-engine.io/base"
	"github.com/shiyanlin/go-engine.io/packet"
)

type conn struct {
	url          url.URL
	remoteHeader http.Header
	ws           wrapper
	closed       chan struct{}
	closeOnce    sync.Once
	base.FrameWriter
	base.FrameReader
}

func newConn(ws *websocket.Conn, url url.URL, header http.Header) base.Conn {
	w := newWrapper(ws)
	closed := make(chan struct{})
	return &conn{
		url:          url,
		remoteHeader: header,
		ws:           w,
		closed:       closed,
		FrameReader:  packet.NewDecoder(w),
		FrameWriter:  packet.NewEncoder(w),
	}
}

func (c *conn) URL() url.URL {
	return c.url
}

func (c *conn) RemoteHeader() http.Header {
	return c.remoteHeader
}

func (c *conn) LocalAddr() net.Addr {
	return c.ws.LocalAddr()
}

func (c *conn) RemoteAddr() net.Addr {
	return c.ws.RemoteAddr()
}

func (c *conn) SetReadDeadline(t time.Time) error {
	return c.ws.SetReadDeadline(t)
}

func (c *conn) SetWriteDeadline(t time.Time) error {
	return c.ws.SetWriteDeadline(t)
}

func (c *conn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	<-c.closed
}

func (c *conn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
	return c.ws.Close()
}
