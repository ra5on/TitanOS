package backup

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/retention"
)

// noSpace recognises what restic and its backends say when the target is
// full or over its quota.
func noSpace(out string) bool {
	lower := strings.ToLower(out)
	for _, sign := range []string{"no space left on device", "disk quota exceeded", "quotaexceeded", "insufficient storage", "insufficientstorage", "507 insufficient", "not enough space"} {
		if strings.Contains(lower, sign) {
			return true
		}
	}
	return false
}

type resticVersion struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	Tags []string  `json:"tags"`
}

// versions lists the versions of a job, by tag group as forget groups them.
func (m *Manager) versions(ctx context.Context, job Job, env, base []string) (map[string][]retention.Version, error) {
	var output strings.Builder
	out, err := m.run(ctx, env, append(slices.Clone(base), "snapshots", "--json", "--tag", "titanos"), func(line string) { output.WriteString(line) })
	if err != nil {
		return nil, errors.New(lastLines(out, job.Password))
	}
	var raw []resticVersion
	if err := json.Unmarshal([]byte(output.String()), &raw); err != nil {
		return nil, err
	}
	groups := map[string][]retention.Version{}
	for _, v := range raw {
		tags := slices.Clone(v.Tags)
		slices.Sort(tags)
		key := strings.Join(tags, ",")
		groups[key] = append(groups[key], retention.Version{ID: v.ID, Time: m.local(v.Time)})
	}
	return groups, nil
}

// forgetChosen removes the versions keepVersions does not keep.
func (m *Manager) forgetChosen(ctx context.Context, job Job, env, base []string) error {
	groups, err := m.versions(ctx, job, env, base)
	if err != nil {
		return err
	}
	now := m.local(m.now())
	var drop []string
	for _, group := range groups {
		keep := retention.Select(job.Keep, group, now)
		for _, v := range group {
			if !keep[v.ID] {
				drop = append(drop, v.ID)
			}
		}
	}
	if len(drop) == 0 {
		return nil
	}
	slices.Sort(drop)
	if out, err := m.run(ctx, env, append(append(slices.Clone(base), "forget", "--prune"), drop...), nil); err != nil {
		return errors.New(lastLines(out, job.Password))
	}
	return nil
}

// dropOldest removes the oldest version of the job (of any tag group), if
// more than one is left, and frees its space. It returns its time.
func (m *Manager) dropOldest(ctx context.Context, job Job, env, base []string) (string, error) {
	groups, err := m.versions(ctx, job, env, base)
	if err != nil {
		return "", err
	}
	var all []retention.Version
	for _, group := range groups {
		// The newest of every group stays.
		all = append(all, retention.OldestFirst(group)...)
	}
	if len(all) == 0 {
		return "", nil
	}
	slices.SortFunc(all, func(a, b retention.Version) int { return a.Time.Compare(b.Time) })
	oldest := all[0]
	if out, err := m.run(ctx, env, append(slices.Clone(base), "forget", "--prune", oldest.ID), nil); err != nil {
		return "", errors.New(lastLines(out, job.Password))
	}
	return oldest.Time.Format("2006-01-02 15:04"), nil
}
