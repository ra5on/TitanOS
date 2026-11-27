package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

func init() {
	registerPersonalTools(func(s *Server) []personalTool {
		tools := []personalTool{{ID: "files.read", Title: "Ausgewählte Dateien lesen", App: "files", Default: pa.Ask, SourceKinds: []string{"file"}, Check: func(_ context.Context, u auth.User, r pa.Request) error {
			if s.opts.Files == nil || len(r.Sources) < 1 || len(r.Sources) > 12 {
				return errors.New("wähle ein bis zwölf Dateien")
			}
			for _, src := range r.Sources {
				if src.Kind != "file" {
					return errors.New("wähle eine Datei")
				}
				if _, err := s.personalFileRoot(u, src, false); err != nil {
					return err
				}
			}
			return nil
		}, Prepare: s.readPersonalFiles}}
		for _, item := range []struct{ id, title, source string }{{"office.inspect", "Gewählten Bereich lesen", "file"}, {"office.copy", "Bearbeitete Kopie erstellen", "file"}, {"office.table", "Tabelle aus Text erstellen", "folder"}, {"office.document", "Word-Entwurf erstellen", "folder"}, {"office.deck", "Präsentation aus Gliederung erstellen", "folder"}} {
			t := personalTool{ID: item.id, Title: item.title, App: "files", Default: pa.Ask, SourceKinds: []string{item.source}, Check: s.personalOfficeAccess, Prepare: func(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, error) {
				p, _, err := s.planPersonalOffice(ctx, u, r)
				return p, err
			}}
			if item.id != "office.inspect" {
				t.Apply = s.applyPersonalOffice
			}
			tools = append(tools, t)
		}
		status := func(u auth.User) error {
			if !u.Role.CanAdminister() || s.opts.Backup == nil {
				return errors.New("der Sicherungsstatus erfordert Verwaltungsrechte")
			}
			return nil
		}
		tools = append(tools, personalTool{ID: "backup.status", Title: "Sicherungsstatus abfragen", App: "backups", Default: pa.Never, Available: status, Check: func(_ context.Context, u auth.User, r pa.Request) error {
			if len(r.Sources) > 0 {
				return errors.New("der Status benötigt keine Dateien")
			}
			return status(u)
		}, Prepare: func(_ context.Context, u auth.User, _ pa.Request) (pa.Preview, error) {
			if err := status(u); err != nil {
				return pa.Preview{}, err
			}
			jobs := s.opts.Backup.List()
			var lines []string
			var sources []pa.Source
			for _, v := range jobs {
				job := v.Job
				stamp := "Kein Lauf aufgezeichnet"
				if v.StateKnown && v.State.LastRun != nil {
					stamp = v.State.LastRun.Format(time.RFC3339)
				}
				switch {
				case !v.StateKnown:
					stamp = "Status unbekannt"
				case v.State.Running:
					stamp += " · Läuft"
				case v.State.Queued:
					stamp += " · Wartet"
				case v.State.LastError != "":
					stamp += " · Fehlgeschlagen"
				case v.State.Warning != "":
					stamp += " · Mit Warnung"
				case v.State.LastOK != nil:
					stamp += " · Erfolgreich"
				}
				// Destination URLs, credentials and private keys stay in the backup manager.
				name := job.Destination.DeviceName
				if name == "" {
					name = job.Destination.Volume
				}
				if name == "" {
					name = job.Destination.TargetID
				}
				target := job.Destination.Type
				if name != "" {
					target += " · " + name
				}
				lines = append(lines, fmt.Sprintf("%s · %s · %s", job.ID, target, stamp))
				sources = append(sources, pa.Source{Kind: "backup", ID: job.ID, Label: job.ID})
			}
			if len(lines) == 0 {
				lines = []string{"Keine Sicherungsaufträge vorhanden."}
			}
			return pa.Preview{Title: "Sicherungsstatus", Text: strings.Join(lines, "\n"), Sources: sources}, nil
		}})
		tools = append(tools, personalTool{ID: "files.search", Title: "Im ausgewählten Ordner suchen", App: "files", Default: pa.Ask, SourceKinds: []string{"folder"}, Check: s.personalSearchAccess, Prepare: s.personalFindFiles})
		return tools
	})
}
