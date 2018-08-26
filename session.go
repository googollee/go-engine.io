package engineio

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/shiyanlin/go-engine.io/base"
	"github.com/shiyanlin/go-engine.io/payload"
	"github.com/shiyanlin/go-engine.io/transport"
)

type session struct {
	params    base.ConnParameters
	manager   *manager
	closeOnce sync.Once
	context   interface{}

	writeLocker sync.RWMutex
	upgradeLocker sync.RWMutex
	transport     string
	conn          base.Conn
}

func newSession(m *manager, t string, conn base.Conn, params base.ConnParameters) (*session, error) {
	params.SID = m.NewID()
	ret := &session{
		transport: t,
		conn:      conn,
		params:    params,
		manager:   m,
	}
	m.Add(ret)
	ret.setDeadline()

	return ret, nil
}

func (s *session) SetContext(v interface{}) {
	s.context = v
}

func (s *session) Context() interface{} {
	return s.context
}

func (s *session) ID() string {
	return s.params.SID
}

func (s *session) Transport() string {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	return s.transport
}

func (s *session) Close() error {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	s.closeOnce.Do(func() {
		s.manager.Remove(s.params.SID)
	})
	return s.conn.Close()
}

func (s *session) NextReader() (FrameType, io.ReadCloser, error) {
	for {
		ft, pt, r, err := s.nextReader()
		if err != nil {
			return 0, nil, err
		}
		switch pt {
		case base.PING:
			err := func() error {
				w, err := s.nextWriter(ft, base.PONG)
				if err != nil {
					return err
				}
				io.Copy(w, r)
				return w.Close()
			}()
			r.Close()
			if err != nil {
				s.Close()
				return 0, nil, err
			}
			s.setDeadline()
		case base.CLOSE:
			r.Close()
			s.Close()
			return 0, nil, io.EOF
		case base.MESSAGE:
			return FrameType(ft), r, nil
		}
		r.Close()
	}
}

func (s *session) NextWriter(typ FrameType) (io.WriteCloser, error) {
	return s.nextWriter(base.FrameType(typ), base.MESSAGE)
}

func (s *session) URL() url.URL {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	return s.conn.URL()
}

func (s *session) LocalAddr() net.Addr {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	return s.conn.LocalAddr()
}

func (s *session) RemoteAddr() net.Addr {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	return s.conn.RemoteAddr()
}

func (s *session) RemoteHeader() http.Header {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	return s.conn.RemoteHeader()
}

func (s *session) nextReader() (base.FrameType, base.PacketType, io.ReadCloser, error) {
	var ft base.FrameType
	var pt base.PacketType
	var r io.ReadCloser
	var err error
	for i := 0; i < 30; i++ {
		s.upgradeLocker.RLock()
		ft, pt, r, err = s.conn.NextReader()
		if err != nil {
			s.upgradeLocker.RUnlock()
			if op, ok := err.(payload.Error); ok {
				if op.Temporary() {
					time.Sleep(50 * time.Millisecond)
					continue
				}
			}
			return 0, 0, nil, err
		}
		return ft, pt, newReader(r, &s.upgradeLocker), nil
	}
	return 0, 0, nil, err
}

func (s *session) nextWriter(ft base.FrameType, pt base.PacketType) (io.WriteCloser, error) {
	var w io.WriteCloser
	var err error
	for i := 0; i < 30; i++ {
		s.writeLocker.Lock()
		w, err = s.conn.NextWriter(ft, pt)
		if err != nil {
			s.writeLocker.Unlock()
			if op, ok := err.(payload.Error); ok {
				if op.Temporary() {
					time.Sleep(50 * time.Millisecond)
					continue
				}
			}
			return nil, err
		}
		return newWriter(w, &s.writeLocker), nil
	}
	return nil, err
}

func (s *session) setDeadline() {
	deadline := time.Now().Add(s.params.PingTimeout)
	var conn base.Conn
	for {
		s.upgradeLocker.RLock()
		same := conn == s.conn
		conn = s.conn
		s.upgradeLocker.RUnlock()

		if same {
			return
		}

		s.conn.SetReadDeadline(deadline)
		s.conn.SetWriteDeadline(deadline)
	}
}

func (s *session) upgrade(transport string, conn base.Conn) {
	go s.upgrading(transport, conn)
}

func (s *session) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.upgradeLocker.RLock()
	conn := s.conn
	s.upgradeLocker.RUnlock()

	if h, ok := conn.(http.Handler); ok {
		h.ServeHTTP(w, r)
	}
}

func (s *session) upgrading(t string, conn base.Conn) {
	deadline := time.Now().Add(s.params.PingTimeout)
	conn.SetReadDeadline(deadline)
	conn.SetWriteDeadline(deadline)

	ft, pt, r, err := conn.NextReader()
	if err != nil {
		conn.Close()
		return
	}
	if pt != base.PING {
		conn.Close()
		return
	}
	if err := r.Close(); err != nil {
		conn.Close()
		return
	}

	w, err := conn.NextWriter(ft, base.PONG)
	if err != nil {
		conn.Close()
		return
	}
	if _, err := io.Copy(w, r); err != nil {
		conn.Close()
		return
	}
	if err := w.Close(); err != nil {
		conn.Close()
		return
	}

	s.upgradeLocker.RLock()
	old := s.conn
	s.upgradeLocker.RUnlock()
	p, ok := old.(transport.Pauser)
	if !ok {
		// old transport doesn't support upgrading
		conn.Close()
		return
	}
	p.Pause()
	defer func() {
		if p != nil {
			p.Resume()
		}
	}()

	_, pt, r, err = conn.NextReader()
	if err != nil {
		conn.Close()
		return
	}
	if pt != base.UPGRADE {
		conn.Close()
		return
	}
	if err := r.Close(); err != nil {
		conn.Close()
		return
	}

	s.upgradeLocker.Lock()
	s.conn = conn
	s.transport = t
	s.upgradeLocker.Unlock()
	p = nil

	old.Close()
}