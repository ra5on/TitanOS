import XCTest
@testable import TitanKit

final class NativeTransportTests: XCTestCase {
	func testLocalEndpointsUsePinnedHTTPS() {
		XCTAssertEqual(Titand.nativeScheme(for: "titan.local"), "https")
		XCTAssertEqual(Titand.nativeScheme(for: "192.168.1.20"), "https")
	}

	func testTailscaleEndpointsUsePinnedHTTPS() {
		XCTAssertEqual(Titand.nativeScheme(for: "100.64.0.1"), "https")
		XCTAssertEqual(Titand.nativeScheme(for: "100.127.255.254"), "https")
	}

	func testPublicAddressesAreNeverClassifiedAsTailscale() {
		XCTAssertEqual(Titand.nativeScheme(for: "100.63.255.255"), "https")
		XCTAssertEqual(Titand.nativeScheme(for: "100.128.0.1"), "https")
	}

	func testLiteralIPv4DetectionIsStrict() {
		XCTAssertTrue(SavedDevice.isIPv4Address("192.168.1.20"))
		XCTAssertTrue(SavedDevice.isIPv4Address("100.90.0.1"))
		XCTAssertFalse(SavedDevice.isIPv4Address("titan.local"))
		XCTAssertFalse(SavedDevice.isIPv4Address("192.168.1"))
		XCTAssertFalse(SavedDevice.isIPv4Address("192.168.1.999"))
		XCTAssertFalse(SavedDevice.isIPv4Address("192.168.01.20"))
		XCTAssertFalse(SavedDevice.isIPv4Address("192.168.01.20.invalid"))
	}

	func testLocalEndpointHostsRejectURLSyntaxAndPublicNames() {
		XCTAssertTrue(SavedDevice.isValidLocalEndpointHost("titan.local"))
		XCTAssertTrue(SavedDevice.isValidLocalEndpointHost("Titan-Pro.local"))
		XCTAssertTrue(SavedDevice.isValidLocalEndpointHost("192.168.1.20"))

		XCTAssertFalse(SavedDevice.isValidLocalEndpointHost("example.com"))
		XCTAssertFalse(SavedDevice.isValidLocalEndpointHost("titan.local/path"))
		XCTAssertFalse(SavedDevice.isValidLocalEndpointHost("user@titan.local"))
		XCTAssertFalse(SavedDevice.isValidLocalEndpointHost("titan.local:443"))
		XCTAssertFalse(SavedDevice.isValidLocalEndpointHost("-titan.local"))
		XCTAssertFalse(SavedDevice.isValidLocalEndpointHost("titan..local"))
	}

	func testAccountAvatarPathAcceptsOnlyTitandContentAddressedRoute() {
		let hash = String(repeating: "a", count: 64)
		XCTAssertTrue(Titand.isValidAccountAvatarPath("/api/accounts/alice-2/avatar/\(hash).webp"))

		XCTAssertFalse(Titand.isValidAccountAvatarPath("https://example.com/avatar.webp"))
		XCTAssertFalse(Titand.isValidAccountAvatarPath("//example.com/avatar.webp"))
		XCTAssertFalse(Titand.isValidAccountAvatarPath("/api/accounts/../avatar/\(hash).webp"))
		XCTAssertFalse(Titand.isValidAccountAvatarPath("/api/accounts/alice/avatar/\(hash).webp?other=1"))
		XCTAssertFalse(Titand.isValidAccountAvatarPath("/api/accounts/alice/avatar/\(hash.uppercased()).webp"))
	}
}
