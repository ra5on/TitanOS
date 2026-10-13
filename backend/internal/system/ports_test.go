package system

import (
	"slices"
	"testing"
)

func TestParseSockets(t *testing.T) {
	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 5120 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1F90 0100007F:D431 01 00000000:00000000 00:00000000 00000000     0        0 5121 1 0000000000000000 20 4 30 10 -1
   2: 00000000000000000000000000000000:0050 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 5122 1 0000000000000000 100 0 0 10 0
`
	if got := parseSockets(tcp, "0A"); !slices.Equal(got, []int{443, 80}) {
		t.Errorf("listening TCP ports = %v", got)
	}
	udp := `   sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops
  512: 00000000:14E9 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 4711 2 0000000000000000 0
`
	if got := parseSockets(udp, ""); !slices.Equal(got, []int{5353}) {
		t.Errorf("bound UDP ports = %v", got)
	}
}
