package gateway

import (
	"io"
	"net/http"
)

func copyBody(w io.Writer, resp *http.Response) (int64, error) { return io.Copy(w, resp.Body) }
