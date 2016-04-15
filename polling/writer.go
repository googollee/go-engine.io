package polling

import (
	"errors"
	"io"
)

func MakeSendChan() chan bool {
	return make(chan bool, 1)
}

type Writer struct {
	io.WriteCloser
	server *Polling
}

func NewWriter(w io.WriteCloser, server *Polling) *Writer {
	return &Writer{
		WriteCloser: w,
		server:      server,
	}
}

func (w *Writer) Close() error {
	if w.server.getState() != stateNormal {
		return errors.New("use of closed network connection")
	}
	w.server.sendChanMu.Lock()
	sendChan := w.server.sendChan
	w.server.sendChanMu.Unlock()
	select {
	case sendChan <- true:
	default:
	}
	return w.WriteCloser.Close()
}
