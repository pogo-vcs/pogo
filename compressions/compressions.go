package compressions

import (
	"errors"
	"io"
	"sync"
)

type closeWrapper struct {
	ReadCloser io.ReadCloser
	Closer     io.Closer
	mut        sync.Mutex
}

func (c *closeWrapper) Close() error {
	c.mut.Lock()
	rc := c.ReadCloser
	cl := c.Closer
	c.ReadCloser = nil
	c.Closer = nil
	c.mut.Unlock()

	var e1, e2 error
	if rc != nil {
		e1 = rc.Close()
	}
	if cl != nil {
		e2 = cl.Close()
	}

	switch {
	case e1 == nil:
		return e2
	case e2 == nil:
		return e1
	default:
		return errors.Join(e1, e2)
	}
}

func (c *closeWrapper) Read(p []byte) (int, error) {
	c.mut.Lock()
	rc := c.ReadCloser
	c.mut.Unlock()

	if rc == nil {
		return 0, io.ErrClosedPipe
	}

	n, err := rc.Read(p)
	if err == io.EOF {
		_ = c.Close()
	}
	return n, err
}