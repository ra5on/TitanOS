package homeassistant

import "testing"

func TestCameraEntitiesAndCommands(t *testing.T) {
	snap := Snapshot{Cameras: []RunValues{{Name: "Hof", Running: true}}}
	// Sensors only: a binary sensor for recording, no switch.
	list := entities(snap, MQTT{Groups: []string{"cameras"}}, "base")
	if len(list) != 1 || list[0].component != "binary_sensor" || list[0].state != "ON" {
		t.Fatalf("sensor: %+v", list)
	}
	// With the command group: a switch that starts and stops recording.
	list = entities(snap, MQTT{Groups: []string{"cameras"}, Commands: []string{"cameras"}}, "base")
	if len(list) != 1 || list[0].component != "switch" || list[0].kind != "camera" || list[0].config["payload_off"] != "stop" {
		t.Fatalf("switch: %+v", list)
	}
	if len(entities(snap, MQTT{}, "base")) != 0 {
		t.Fatal("cameras published without the group")
	}
	if !allowedCommand(MQTT{Commands: []string{"cameras"}}, "camera", "Hof", "stop") ||
		allowedCommand(MQTT{Commands: []string{"vms"}}, "camera", "Hof", "stop") ||
		allowedCommand(MQTT{Commands: []string{"cameras"}}, "camera", "Hof", "delete") {
		t.Fatal("camera commands not limited to the group and to start/stop")
	}
}
