package backup

import (
	"context"
	"errors"
	"os"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// personalFile finishes and authenticates the private archive before restic
// sees it. Only age ciphertext is staged, in an unlinked file on a data
// volume. Failed exports therefore cannot publish an incomplete snapshot.
func (m *Manager) personalFile(ctx context.Context, job Job) (*os.File, func(), error) {
	if m.PersonalExport == nil {
		return nil, nil, errors.New("personal archives are unavailable")
	}
	root, err := volumedata.Open(m.mountRoot, m.homeVolume(job), "backup", m.mounted())
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	f, err := volumedata.UnlinkedTemp(root)
	if err != nil {
		return nil, nil, err
	}
	local := ""
	if job.Destination.Type == "local" {
		local = job.Destination.Volume
	}
	if err = m.PersonalExport(ctx, f, *job.Personal, local); err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}
