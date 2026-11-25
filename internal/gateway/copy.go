package gateway

import (
	"bytes"
	"io"
	"net/http"
)

func copyBody(w io.Writer, resp *http.Response) (int64, error) { return io.Copy(w, resp.Body) }

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
