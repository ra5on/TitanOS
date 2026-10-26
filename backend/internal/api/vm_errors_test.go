package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/docker"
)

func TestVMStartRefusedByDockerIsAConflict(t *testing.T) {
	w := httptest.NewRecorder()
	writeVMError(w, fmt.Errorf("%w: Docker is starting app x on card", docker.ErrHardwareBusy))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "vm_hardware_busy") {
		t.Fatal(w.Code, w.Body.String())
	}
}
