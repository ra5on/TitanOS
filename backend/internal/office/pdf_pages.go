// Selected PDF pages cross the parser boundary as length-prefixed bytes. The
// confined child owns its scratch directory; source/destination NAS paths
// and account credentials never cross this boundary.
package office

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/sandbox"
)

type SelectedPDFPage struct {
	Data   []byte
	Number int
}

const pdfBytes = 50 << 20

func SelectPDFPages(ctx context.Context, pages []SelectedPDFPage) ([]byte, error) {
	var input bytes.Buffer
	if len(pages) < 1 || len(pages) > 20 {
		return nil, errors.New("bitte ein bis zwanzig PDF-Seiten auswählen")
	}
	if err := binary.Write(&input, binary.BigEndian, uint32(len(pages))); err != nil {
		return nil, err
	}
	for _, p := range pages {
		if p.Number < 1 || p.Number > 10000 || !bytes.HasPrefix(p.Data, []byte("%PDF-")) || len(p.Data)+input.Len() > pdfBytes {
			return nil, errors.New("ungültige PDF-Auswahl oder mehr als 50 MiB")
		}
		if err := binary.Write(&input, binary.BigEndian, [2]uint32{uint32(p.Number), uint32(len(p.Data))}); err != nil {
			return nil, err
		}
		input.Write(p.Data)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := sandbox.Command(ctx, sandbox.Limits{}, exe, "-selected-pdf-pages")
	out := &limitedOutput{limit: pdfBytes}
	cmd.Stdin = &input
	cmd.Stdout = out
	cmd.Stderr = &limitedOutput{limit: 4096}
	if err = cmd.Run(); err != nil || !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		return nil, errors.New("ausgewählte PDF-Seiten konnten nicht übernommen werden")
	}
	return out.Bytes(), nil
}

// FilterPDFPages is the fixed-purpose child invoked by SelectPDFPages. It
// accepts page numbers and bytes only. Scratch objects have no caller names.
func FilterPDFPages(ctx context.Context, input io.Reader, output io.Writer) error {
	var count uint32
	if err := binary.Read(input, binary.BigEndian, &count); err != nil {
		return err
	}
	if count < 1 || count > 20 {
		return errors.New("invalid page count")
	}
	dir, err := os.MkdirTemp("", "titanos-pdf-pages-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var selected []string
	var total uint64
	for idx := range count {
		var h [2]uint32
		if err = binary.Read(input, binary.BigEndian, &h); err != nil {
			return err
		}
		total += uint64(h[1])
		if h[0] < 1 || h[0] > 10000 || h[1] < 5 || total > pdfBytes {
			return errors.New("invalid selection")
		}
		f, e := os.CreateTemp(dir, "input-")
		if e != nil {
			return e
		}
		_, e = io.CopyN(f, input, int64(h[1]))
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		page := strconv.Itoa(int(h[0]))
		prefix := filepath.Join(dir, fmt.Sprintf("part-%d-", idx))
		if e = exec.CommandContext(ctx, "pdfseparate", "-f", page, "-l", page, f.Name(), prefix+"%d.pdf").Run(); e != nil {
			return e
		}
		part := prefix + page + ".pdf"
		st, e := os.Stat(part)
		if e != nil {
			return e
		}
		if st.Size() > pdfBytes {
			return ErrTooLarge
		}
		selected = append(selected, part)
	}
	result := selected[0]
	if len(selected) > 1 {
		result = filepath.Join(dir, "result.pdf")
		if err = exec.CommandContext(ctx, "pdfunite", append(selected, result)...).Run(); err != nil {
			return err
		}
	}
	f, err := os.Open(result)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() > pdfBytes {
		return ErrTooLarge
	}
	_, err = io.CopyN(output, f, st.Size())
	return err
}
