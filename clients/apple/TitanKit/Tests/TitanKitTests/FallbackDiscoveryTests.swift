import XCTest

@testable import TitanKit

final class FallbackDiscoveryTests: XCTestCase {
	func testFallbackHostsStaySmallAndPredictable() {
		XCTAssertEqual(
			Titand.fallbackDiscoveryHosts,
			[
				"titan.local",
				"titan-2.local",
				"titan-3.local",
				"titan-4.local",
				"titan-5.local",
				"titan-home.local",
				"titan-pro.local",
			]
		)
	}

	func testFallbackResponseRequiresTheTitanOSVersionContract() throws {
		let valid = Data(#"{"result":{"data":{"version":"1.7.4","name":"titanOS 1.7.4"}}}"#.utf8)
		XCTAssertNoThrow(try Titand.validateFallbackSystemVersion(data: valid, status: 200))

		let unrelated = Data(#"{"result":{"data":{"version":"1.7.4","name":"Another service"}}}"#.utf8)
		XCTAssertThrowsError(try Titand.validateFallbackSystemVersion(data: unrelated, status: 200))

		let incomplete = Data(#"{"result":{"data":{"version":"1.7.4"}}}"#.utf8)
		XCTAssertThrowsError(try Titand.validateFallbackSystemVersion(data: incomplete, status: 200))
	}

	func testLiveFallbackDiscovery() async throws {
		try XCTSkipUnless(
			ProcessInfo.processInfo.environment["TITANKIT_FALLBACK_DISCOVERY_TEST"] == "1",
			"Set TITANKIT_FALLBACK_DISCOVERY_TEST=1 on a network with legacy and current Titans"
		)

		let result = await Titand.discoverFallbackHosts()
		XCTAssertFalse(result.isEmpty, "Expected at least one common Titan hostname to answer")
	}
}
