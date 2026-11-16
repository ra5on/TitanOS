package agent

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
)

// Projects, skills and schedules change in one step under the store's lock
// (1.01): many changes at the same time all arrive, none writes over
// another, and a refused change leaves the file as it is.
func TestStoreUpdatesDoNotLoseChanges(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Open: func() (*os.Root, error) { return os.OpenRoot(dir) }}
	const n = 25
	var wg sync.WaitGroup
	errs := make(chan error, 3*n)
	for i := 0; i < n; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			errs <- s.UpdateProjects("alex", func(list []Project) ([]Project, error) {
				return append(list, Project{ID: NewID(), Name: fmt.Sprintf("P%d", i)}), nil
			})
		}()
		go func() {
			defer wg.Done()
			errs <- s.UpdateSkills("alex", func(list []Skill) ([]Skill, error) {
				return append(list, Skill{ID: NewID(), Name: fmt.Sprintf("S%d", i)}), nil
			})
		}()
		go func() {
			defer wg.Done()
			errs <- s.UpdateSchedules("alex", func(list []Schedule) ([]Schedule, error) {
				return append(list, Schedule{ID: NewID(), Name: fmt.Sprintf("A%d", i), Every: "daily", At: "08:00"}), nil
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	projects, _ := s.Projects("alex")
	skills, _ := s.Skills("alex")
	schedules, _ := s.Schedules("alex")
	if len(projects) != n || len(skills) != n || len(schedules) != n {
		t.Fatalf("projects %d skills %d schedules %d, want %d each", len(projects), len(skills), len(schedules), n)
	}
	// Read back from the volume, not only from RAM.
	fresh := &Store{Open: s.Open}
	if again, _ := fresh.Schedules("alex"); len(again) != n {
		t.Fatalf("schedules on the volume: %d", len(again))
	}
	// A refused change and ErrUnchanged write nothing.
	refused := errors.New("full")
	if err := s.UpdateSkills("alex", func([]Skill) ([]Skill, error) { return nil, refused }); !errors.Is(err, refused) {
		t.Fatalf("refused: %v", err)
	}
	if err := s.UpdateSchedules("alex", func([]Schedule) ([]Schedule, error) { return nil, ErrUnchanged }); err != nil {
		t.Fatalf("unchanged: %v", err)
	}
	if skills, _ := s.Skills("alex"); len(skills) != n {
		t.Fatalf("a refused change wrote: %d skills", len(skills))
	}
	if schedules, _ := s.Schedules("alex"); len(schedules) != n {
		t.Fatalf("ErrUnchanged wrote: %d schedules", len(schedules))
	}
}
