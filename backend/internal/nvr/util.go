package nvr

import (
	"encoding/json"
	"io"
	"time"
)

func nowPlus(d time.Duration) time.Time { return time.Now().Add(d) }

func readFull(r io.Reader, buf []byte) (int, error) { return io.ReadFull(r, buf) }

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
