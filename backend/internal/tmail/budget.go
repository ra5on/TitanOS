package tmail

import (
	"net"
	"sync/atomic"
)

// The client of go-imap (and textproto under go-smtp) reads whatever a
// server sends: a literal, a quoted string or a line of any length is
// kept in memory whole. A hostile or broken server could send gigabytes
// and take titand down, again after every start through the permanent
// connection (security review of 1.05). Every connection of T-Mail
// therefore reads through a budget: before each command the caller says
// how much the answer may be; a server that sends more is cut off and the
// command ends with ErrTooLarge.

// Budgets of the answers.
const (
	// budgetLogin covers greeting, STARTTLS, capabilities and login.
	budgetLogin = 1 << 20
	// budgetCommand covers LIST, SEARCH, STATUS, STORE, MOVE, the heads
	// of a page of mails and what the permanent connection receives;
	// small, since many connections of an account add up (follow-up of
	// the security review).
	budgetCommand = 4 << 20
	// budgetTail is what may follow a downloaded literal in its response:
	// its closing parenthesis and the end of the command.
	budgetTail = 64 << 10
	// budgetStructure covers an answer with BODYSTRUCTURE (and the head
	// of one mail): decoding it takes about twelve times its size.
	budgetStructure = 1 << 20
	// budgetMargin is added to the expected size of a part.
	budgetMargin = 1 << 20
	// budgetSMTP covers a whole SMTP conversation; smtpLines its lines.
	budgetSMTP = 1 << 20
	smtpLines  = 2000
)

// budgetConn counts what is read from a connection (below TLS) against
// the budget of the current command and closes the connection when it is
// used up. lines > 0 also counts line ends.
type budgetConn struct {
	net.Conn
	left     atomic.Int64
	lines    atomic.Int64
	countLn  atomic.Bool
	exceeded atomic.Bool
}

func newBudgetConn(c net.Conn, budget int64) *budgetConn {
	b := &budgetConn{Conn: c}
	b.left.Store(budget)
	return b
}

// set gives the next command its budget.
func (b *budgetConn) set(budget int64) {
	if b != nil {
		b.left.Store(budget)
	}
}

// setLines limits the line ends read until the next set.
func (b *budgetConn) setLines(n int64) {
	b.lines.Store(n)
	b.countLn.Store(true)
}

func (b *budgetConn) Read(p []byte) (int, error) {
	n, err := b.Conn.Read(p)
	if n > 0 {
		over := b.left.Add(-int64(n)) < 0
		if !over && b.countLn.Load() {
			var ends int64
			for _, c := range p[:n] {
				if c == '\n' {
					ends++
				}
			}
			over = b.lines.Add(-ends) < 0
		}
		if over {
			b.exceeded.Store(true)
			_ = b.Conn.Close() // the server sent more than any answer may be
			return 0, ErrTooLarge
		}
	}
	return n, err
}

// tooLarge tells whether the connection was cut off for its budget.
func (b *budgetConn) tooLarge() bool { return b != nil && b.exceeded.Load() }
