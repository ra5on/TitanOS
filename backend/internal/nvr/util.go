package nvr

import (
	"io"
	"time"
)

func nowPlus(d time.Duration) time.Time { return time.Now().Add(d) }

func readFull(r io.Reader, buf []byte) (int, error) { return io.ReadFull(r, buf) }
