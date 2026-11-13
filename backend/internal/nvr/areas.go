package nvr

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Zones and masks drawn into a camera's picture (PLAN 88). Points are
// relative to the picture the detection reads, 0 to 1 from the left and
// from the top, so they hold when the camera's resolution changes. In
// config.yml they become Frigate's zones (cameras.<id>.zones), motion
// masks (motion.mask) and object masks (objects.mask), all as relative
// coordinates "x1,y1,x2,y2,…" (Frigate 0.18 keeps masks and zones
// relative: config/camera/zone.py, util/config.py).
//
// Frigate tells pixels from relative coordinates by comparing each number
// as a string with "1.0" (anything greater is a pixel): "1.000" or a
// number with an exponent would be read as pixels and fail. So every
// number is rounded to 3 places and written in its shortest form (1 as
// "1", never an exponent): coordinate.

// Point is a place in the picture: [x, y], each 0 to 1.
type Point [2]float64

// Zone is a named area of the picture. Events can be limited to the
// zones (Camera.ZonesOnly) and messages to some of them (Notify.Zones).
type Zone struct {
	// ID is the zone's stable name, made from Name when it is drawn; in
	// Frigate it is "z-<id>" (zoneKey).
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Points []Point `json:"points"`
}

// Mask is an area that is left out: of the motion detection (motion
// masks: trees, a clock in the picture) or of the object detection
// (object masks: the street with parked cars).
type Mask struct {
	Points []Point `json:"points"`
}

// Masks are the camera's masks.
type Masks struct {
	Motion  []Mask `json:"motion"`
	Objects []Mask `json:"objects"`
}

// Limits of the areas of one camera.
const (
	MaxZones      = 10
	MaxMasks      = 10
	minPoints     = 3
	MaxAreaPoints = 32
	// minArea is the least area of a polygon (a share of the picture):
	// less is a line or a point, not an area.
	minArea = 0.0001
)

// zoneIDRE: like a camera ID; Frigate's key adds "z-" (zoneKey).
var zoneIDRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// zoneKey is the zone's key in Frigate. A camera ID never has "-", so a
// zone never has a camera's name: Frigate refuses that
// (ensure_zones_and_cameras_have_different_names), and its topics
// "<zone>/<label>" would read as the camera's.
func zoneKey(id string) string { return "z-" + id }

// zoneOfKey is the zone ID of a key of Frigate's; "" if it is none.
func zoneOfKey(key string) string {
	id, ok := strings.CutPrefix(key, "z-")
	if !ok || !zoneIDRE.MatchString(id) {
		return ""
	}
	return id
}

// coordinate rounds a coordinate to 3 places.
func coordinate(v float64) float64 { return math.Round(v*1000) / 1000 }

// formatCoordinate writes a coordinate as Frigate reads it as relative:
// at most 3 places, no trailing zeros, no exponent ("0", "0.5", "1").
func formatCoordinate(v float64) string {
	return strconv.FormatFloat(coordinate(v), 'f', -1, 64)
}

// coordinates is a polygon as Frigate's "x1,y1,x2,y2,…".
func coordinates(points []Point) string {
	parts := make([]string, 0, 2*len(points))
	for _, p := range points {
		parts = append(parts, formatCoordinate(p[0]), formatCoordinate(p[1]))
	}
	return strings.Join(parts, ",")
}

// cleanPolygon checks a polygon and returns it rounded: 3 to
// MaxAreaPoints points, each within the picture, no point twice in a row,
// and an area (not a line).
func cleanPolygon(points []Point, field string) ([]Point, error) {
	if len(points) < minPoints || len(points) > MaxAreaPoints {
		return nil, invalid("%s: %d to %d points", field, minPoints, MaxAreaPoints)
	}
	out := make([]Point, 0, len(points))
	for _, p := range points {
		for _, v := range p {
			if math.IsNaN(v) || v < 0 || v > 1 {
				return nil, invalid("%s: points from 0 to 1", field)
			}
		}
		q := Point{coordinate(p[0]), coordinate(p[1])}
		if len(out) > 0 && out[len(out)-1] == q {
			continue
		}
		out = append(out, q)
	}
	if len(out) > 1 && out[0] == out[len(out)-1] {
		out = out[:len(out)-1]
	}
	if len(out) < minPoints || polygonArea(out) < minArea {
		return nil, invalid("%s: not an area", field)
	}
	return out, nil
}

// polygonArea is the area of a polygon (shoelace), as a share of the
// picture.
func polygonArea(points []Point) float64 {
	sum := 0.0
	for i, p := range points {
		q := points[(i+1)%len(points)]
		sum += p[0]*q[1] - q[0]*p[1]
	}
	return math.Abs(sum) / 2
}

// validateAreas checks the zones and masks of a camera and returns them in
// their stored form: a zone without an ID gets one made from its name.
func validateAreas(c Camera) (Camera, error) {
	if len(c.Zones) > MaxZones {
		return c, invalid("zones: at most %d", MaxZones)
	}
	zones := make([]Zone, 0, len(c.Zones))
	taken := []string{}
	for _, z := range c.Zones {
		if z.ID != "" {
			taken = append(taken, z.ID)
		}
	}
	for i, z := range c.Zones {
		if !validText(z.Name, 40) {
			return c, invalid("zones[%d].name", i)
		}
		if slices.ContainsFunc(zones, func(other Zone) bool { return strings.EqualFold(other.Name, z.Name) }) {
			return c, invalid("zones[%d].name: twice", i)
		}
		if z.ID == "" {
			z.ID = SlugID(z.Name, taken)
			if len(z.ID) > 32 || !zoneIDRE.MatchString(z.ID) {
				z.ID = SlugID("bereich", taken)
			}
			taken = append(taken, z.ID)
		}
		if !zoneIDRE.MatchString(z.ID) || slices.ContainsFunc(zones, func(other Zone) bool { return other.ID == z.ID }) {
			return c, invalid("zones[%d].id", i)
		}
		points, err := cleanPolygon(z.Points, "zones["+strconv.Itoa(i)+"].points")
		if err != nil {
			return c, err
		}
		zones = append(zones, Zone{ID: z.ID, Name: z.Name, Points: points})
	}
	c.Zones = zones
	if len(c.Zones) == 0 {
		c.Zones = nil
	}
	if c.ZonesOnly && len(c.Zones) == 0 {
		return c, invalid("zonesOnly: no zone drawn")
	}
	for _, list := range []struct {
		masks *[]Mask
		field string
	}{{&c.Masks.Motion, "masks.motion"}, {&c.Masks.Objects, "masks.objects"}} {
		if len(*list.masks) > MaxMasks {
			return c, invalid("%s: at most %d", list.field, MaxMasks)
		}
		clean := []Mask{}
		for i, mask := range *list.masks {
			points, err := cleanPolygon(mask.Points, list.field+"["+strconv.Itoa(i)+"]")
			if err != nil {
				return c, err
			}
			clean = append(clean, Mask{Points: points})
		}
		if len(clean) == 0 {
			clean = nil
		}
		*list.masks = clean
	}
	// The zones messages are limited to must be drawn: a zone that is gone
	// would silently widen the messages to the whole picture.
	limit := []string{}
	for _, id := range c.Notify.Zones {
		if !slices.ContainsFunc(c.Zones, func(z Zone) bool { return z.ID == id }) {
			return c, invalid("notify.zones: no zone %q", id)
		}
		if !slices.Contains(limit, id) {
			limit = append(limit, id)
		}
	}
	slices.Sort(limit)
	if len(limit) == 0 {
		limit = nil
	}
	c.Notify.Zones = limit
	return c, nil
}

// zonesBlock is the camera's zones in config.yml: "z-<id>" with the name
// for Frigate's own pages and the polygon.
func zonesBlock(zones []Zone) ymap {
	out := ymap{}
	for _, z := range zones {
		out = append(out, yentry{zoneKey(z.ID), ymap{{"friendly_name", z.Name}, {"coordinates", coordinates(z.Points)}}})
	}
	return out
}

// masksBlock is a dict of masks as Frigate 0.18 wants it (names of its
// own, "mask_1", …).
func masksBlock(masks []Mask) ymap {
	out := ymap{}
	for i, mask := range masks {
		out = append(out, yentry{"mask_" + strconv.Itoa(i+1), ymap{{"coordinates", coordinates(mask.Points)}}})
	}
	return out
}

// zoneKeys are the keys of all zones of a camera in Frigate.
func zoneKeys(zones []Zone) []string {
	out := []string{}
	for _, z := range zones {
		out = append(out, zoneKey(z.ID))
	}
	return out
}

// eventZones turns the zones Frigate names for an event (its keys) into
// the camera's zone IDs and names; keys of zones that are no longer drawn
// are left out.
func eventZones(c Camera, keys []string) (ids, names []string) {
	ids, names = []string{}, []string{}
	for _, key := range keys {
		id := zoneOfKey(key)
		at := slices.IndexFunc(c.Zones, func(z Zone) bool { return z.ID == id })
		if id == "" || at < 0 || slices.Contains(ids, id) {
			continue
		}
		ids, names = append(ids, id), append(names, c.Zones[at].Name)
	}
	return ids, names
}

// shownInZones tells whether an event of the camera is shown: always,
// unless the camera shows only events in its zones and the object entered
// none (yet).
func shownInZones(c Camera, zoneIDs []string) bool {
	return !c.ZonesOnly || len(zoneIDs) > 0
}

// toldInZones tells whether a message about an event may go out as far
// as zones go: the camera's events must be shown, and a camera that
// limits its messages to zones wants one of them.
func toldInZones(c Camera, zoneIDs []string) bool {
	if !shownInZones(c, zoneIDs) {
		return false
	}
	if len(c.Notify.Zones) == 0 {
		return true
	}
	return slices.ContainsFunc(zoneIDs, func(id string) bool { return slices.Contains(c.Notify.Zones, id) })
}
