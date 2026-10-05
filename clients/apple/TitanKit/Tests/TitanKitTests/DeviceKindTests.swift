import Testing
@testable import TitanKit

@Suite("Titan device kind")
struct DeviceKindTests {
	@Test("Recognizes every supported hardware family", arguments: [
		("Umbrel Home (2025)", TitanDeviceKind.home),
		("Umbrel Pro", TitanDeviceKind.pro),
		("Raspberry Pi 5", TitanDeviceKind.raspberryPi),
		("Raspberry Pi 4", TitanDeviceKind.raspberryPi),
		("Standard PC (Q35 + ICH9, 2009)", TitanDeviceKind.generic),
	])
	func recognizedModel(model: String, expected: TitanDeviceKind) {
		#expect(TitanDeviceKind(model: model) == expected)
	}

	@Test("Missing model uses generic hardware")
	func missingModel() {
		#expect(TitanDeviceKind(model: nil) == .generic)
	}
}
