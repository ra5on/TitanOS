package webapps

import (
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/docker"
)

// Docker versions of the Craft apps (PLAN 66, owner 7.11.2026): after the
// update TitanOS offers to install the web apps and to remove the Docker
// apps they replace in one go.

// Container is what detection needs of a Docker container.
type Container struct {
	// App is the Compose project (the TitanOS app) it belongs to.
	App   string
	Name  string
	Image string
	State string
}

// DockerApp is a TitanOS Docker app that is a Craft app.
type DockerApp struct {
	// App is the name the removal routes use (DELETE /api/v1/docker/apps/{name}).
	App string `json:"app"`
	// Webapp is the catalog id that replaces it.
	Webapp  string `json:"webapp"`
	Running bool   `json:"running"`
	Image   string `json:"image"`
}

// nameVariants are the names a Craft app goes by as a container or app.
// "-server" is not one: FigCraft's collaboration server is not the app.
func nameVariants(id string) []string {
	return []string{id, id + "-web", id + "-app", id + "_web"}
}

// imageWebapp is the catalog id an image is a build of, or "".
func imageWebapp(image string) string {
	ref := strings.ToLower(strings.TrimSpace(image))
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	parts := strings.Split(ref, "/")
	if len(parts) > 1 && (strings.ContainsAny(parts[0], ".:") || parts[0] == "localhost") {
		parts = parts[1:]
	}
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return ""
	}
	last := parts[len(parts)-1]
	for _, app := range catalog {
		// storytold/<id>… (Docker Hub or ghcr.io) and plain <id> images.
		for i, part := range parts[:len(parts)-1] {
			if part == "storytold" && slices.Contains(nameVariants(app.ID), parts[i+1]) {
				return app.ID
			}
		}
		if slices.Contains(nameVariants(app.ID), last) {
			return app.ID
		}
	}
	return ""
}

// nameWebapp is the catalog id an app or container name stands for, or "".
func nameWebapp(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, app := range catalog {
		if slices.Contains(nameVariants(app.ID), name) {
			return app.ID
		}
	}
	return ""
}

// DetectDocker finds the TitanOS Docker apps (managed: their compose file
// is kept by TitanOS, so the normal removal works) that are Craft apps:
// every container of the app is a build of the same Craft app (its image
// or its name), or the app has no container and its name is one. An app
// with any other container (a database, a proxy) is never offered.
func DetectDocker(managed []string, containers []Container) []DockerApp {
	var out []DockerApp
	for _, app := range managed {
		var own []Container
		for _, c := range containers {
			if c.App == app {
				own = append(own, c)
			}
		}
		found := DockerApp{App: app}
		ok := true
		if len(own) == 0 {
			found.Webapp = nameWebapp(app)
			ok = found.Webapp != ""
		}
		for _, c := range own {
			id := imageWebapp(c.Image)
			if id == "" {
				id = nameWebapp(strings.TrimPrefix(c.Name, "/"))
				if id == "" || imageNamesOther(c.Image) {
					ok = false
					break
				}
			}
			if found.Webapp != "" && found.Webapp != id {
				ok = false
				break
			}
			found.Webapp = id
			if found.Image == "" {
				found.Image = c.Image
			}
			if c.State == "running" {
				found.Running = true
			}
		}
		if ok && found.Webapp != "" {
			out = append(out, found)
		}
	}
	return out
}

// imageNamesOther: an image that names a known other program (a database,
// a web server) makes a container named like a Craft app no Craft app.
func imageNamesOther(image string) bool {
	ref := strings.ToLower(image)
	for _, other := range []string{"postgres", "mysql", "mariadb", "redis", "mongo", "valkey", "server"} {
		if strings.Contains(ref, other) {
			return true
		}
	}
	return false
}

// DetectDockerStatus is DetectDocker on Docker's status (managed apps and
// containers); nothing while Docker does not answer.
func DetectDockerStatus(status docker.Status) []DockerApp {
	// Without Docker's container list every app looks empty; then nothing
	// is offered rather than judging by names alone.
	if !status.Running {
		return nil
	}
	containers := make([]Container, 0, len(status.Containers))
	for _, c := range status.Containers {
		containers = append(containers, Container{App: c.App, Name: c.Name, Image: c.Image, State: c.State})
	}
	return DetectDocker(status.Apps, containers)
}
