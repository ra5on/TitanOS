package tmail

import (
	"fmt"
	"io"
	"slices"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// UID lists of a server are read range by range and never expanded whole:
// "1:4000000000" in a SEARCH answer is a few bytes, its UIDs are not
// (security review of 1.05).

// maxUIDs bounds the UIDs taken from one answer.
const maxUIDs = 100000

// uidRanges returns the ranges of a set of UIDs, low to high bounds.
func uidRanges(set imap.NumSet) []imap.UIDRange {
	uids, ok := set.(imap.UIDSet)
	if !ok {
		return nil
	}
	out := make([]imap.UIDRange, 0, len(uids))
	for _, r := range uids {
		lo, hi := r.Start, r.Stop
		if hi == 0 {
			hi = lo
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		if lo == 0 {
			lo = 1
		}
		if hi >= lo {
			out = append(out, imap.UIDRange{Start: lo, Stop: hi})
		}
	}
	return out
}

// uidCount is the number of UIDs of a set.
func uidCount(set imap.NumSet) int {
	var n uint64
	for _, r := range uidRanges(set) {
		n += uint64(r.Stop-r.Start) + 1
	}
	return int(min(n, 1<<31-1))
}

// uidsDesc returns at most limit UIDs of a set below before (0: all),
// highest first.
func uidsDesc(set imap.NumSet, before uint32, limit int) []uint32 {
	limit = min(limit, maxUIDs)
	var out []uint32
	ranges := uidRanges(set)
	slices.SortFunc(ranges, func(a, b imap.UIDRange) int { return int(int64(b.Stop) - int64(a.Stop)) })
	seen := map[uint32]bool{}
	for _, r := range ranges {
		hi := uint64(r.Stop)
		if before != 0 && hi >= uint64(before) {
			if uint64(before) <= uint64(r.Start) {
				continue
			}
			hi = uint64(before) - 1
		}
		for u := hi; u >= uint64(r.Start) && len(out) < limit; u-- {
			if !seen[uint32(u)] {
				seen[uint32(u)] = true
				out = append(out, uint32(u))
			}
		}
	}
	slices.SortFunc(out, func(a, b uint32) int { return int(int64(b) - int64(a)) })
	return out
}

// uidsFrom returns at most limit UIDs of a set from from on, lowest
// first.
func uidsFrom(set imap.NumSet, from uint32, limit int) []uint32 {
	limit = min(limit, maxUIDs)
	var out []uint32
	ranges := uidRanges(set)
	slices.SortFunc(ranges, func(a, b imap.UIDRange) int { return int(int64(a.Start) - int64(b.Start)) })
	for _, r := range ranges {
		lo := max(uint32(r.Start), from)
		for u := uint64(lo); u <= uint64(r.Stop) && len(out) < limit; u++ {
			if len(out) == 0 || out[len(out)-1] != uint32(u) {
				out = append(out, uint32(u))
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// maxUID is the highest UID of a set (0 if empty).
func maxUID(set imap.NumSet) uint32 {
	var top uint32
	for _, r := range uidRanges(set) {
		top = max(top, uint32(r.Stop))
	}
	return top
}

// collect reads the answers of a FETCH one mail after the other; the
// budget of the connection bounds them.
func collect(cmd *imapclient.FetchCommand) ([]*imapclient.FetchMessageBuffer, error) {
	var out []*imapclient.FetchMessageBuffer
	for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
		buf, err := msg.Collect()
		if err != nil {
			_ = cmd.Close() // the error of the mail is what counts
			return nil, err
		}
		out = append(out, buf)
	}
	return out, cmd.Close()
}

// uidSearch runs a UID SEARCH; compact asks for the ranges of ESEARCH
// where the server has it. An ESEARCH answer without "UID" holds sequence
// numbers, not UIDs: it is not taken as empty but asked again without
// ESEARCH; a second such answer is an error (third review of 1.05).
func uidSearch(c *imapclient.Client, criteria *imap.SearchCriteria, compact bool) (*imap.SearchData, error) {
	var opts *imap.SearchOptions
	if compact {
		opts = searchOptions(c)
	}
	data, err := c.UIDSearch(criteria, opts).Wait()
	if err != nil {
		return nil, err
	}
	if _, seq := data.All.(imap.SeqSet); !seq {
		return data, nil
	}
	if opts != nil {
		if data, err = c.UIDSearch(criteria, nil).Wait(); err != nil {
			return nil, err
		}
		if _, seq := data.All.(imap.SeqSet); !seq {
			return data, nil
		}
	}
	return nil, errSearchWithoutUIDs
}

// errSearchWithoutUIDs: the server answered a UID SEARCH with sequence
// numbers.
var errSearchWithoutUIDs = fmt.Errorf("%w: the server answered a UID SEARCH without UIDs", ErrUnreachable)

// searchOptions asks for the compact answer (ESEARCH: ranges instead of
// every UID) where the server has it, so that a big folder fits the
// budget of a command.
func searchOptions(c *imapclient.Client) *imap.SearchOptions {
	if c.Caps().Has(imap.CapESearch) || c.Caps().Has(imap.CapIMAP4rev2) {
		return &imap.SearchOptions{ReturnAll: true}
	}
	return nil
}

// abandon gives a connection up in the middle of an answer. go-imap's
// reader waits until a literal it handed out has been read and until its
// channels are taken from, so a Close from the reading side alone would
// wait for ever (a browser that went away during a download, a server
// that sends more than asked for). The connection closes in the
// background while drain reads the rest away; with the connection closed
// that ends at once.
func abandon(c *imapclient.Client, drain func()) {
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_ = c.Close() // given up; its error says nothing new
	}()
	drain()
	<-closed
}

// drainFetch reads away what is left of a FETCH: the literals given, the
// rest of the current mail and the mails after it.
func drainFetch(cmd *imapclient.FetchCommand, msg *imapclient.FetchMessageData, literals ...io.Reader) func() {
	return func() {
		for _, lit := range literals {
			_, _ = io.Copy(io.Discard, lit) // ends with the connection
		}
		for ; msg != nil; msg = cmd.Next() {
			for item := msg.Next(); item != nil; item = msg.Next() {
				if section, ok := item.(imapclient.FetchItemDataBodySection); ok && section.Literal != nil {
					_, _ = io.Copy(io.Discard, section.Literal)
				}
			}
		}
		_ = cmd.Close() // the connection is gone; so is the answer
	}
}
