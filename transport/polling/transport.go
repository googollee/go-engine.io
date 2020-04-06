package polling

import (
	"net/http"
	"net/url"
	"time"

	"github.com/googollee/go-engine.io/base"
)

// Transport is the transport of polling.
type Transport struct {
	Client      *http.Client
	CheckOrigin func(r *http.Request) bool
	url         *url.URL
}

// Default is the default transport.
var Default = &Transport{
	Client: &http.Client{
		Timeout: time.Minute,
	},
	CheckOrigin: nil,
}

// Name is the name of transport.
func (t *Transport) Name() string {
	return "polling"
}

// Accept accepts a http request and create Conn.
func (t *Transport) Accept(w http.ResponseWriter, r *http.Request) (base.Conn, error) {
	conn := newServerConn(t, r)
	return conn, nil
}

// Dial dials connection to url.
func (t *Transport) Dial(u *url.URL, requestHeader http.Header) (base.Conn, error) {
	query := u.Query()
	v := t.url.Query()
	for key, value := range v {
		query.Set(key, value[0])
	}
	query.Set("transport", t.Name())
	u.RawQuery = query.Encode()

	client := t.Client
	if client == nil {
		client = Default.Client
	}

	return dial(client, u, requestHeader)
}

func (t *Transport) SetURL(url *url.URL) {
	t.url = url
}

func (t *Transport) GetURL() *url.URL {
	return t.url
}
