@testable import TitanKit
import XCTest

final class SavedDeviceDiscoveryTests: XCTestCase {
	func testBonjourCandidateCannotIntroduceTailscaleAddress() throws {
		let candidate = Candidate(
			host: "titan.local",
			addresses: ["192.168.1.20", "100.90.0.1"],
			name: "Titan"
		)

		let filtered = try XCTUnwrap(Titand.localDiscoveryCandidate(candidate))

		XCTAssertEqual(filtered.host, "titan.local")
		XCTAssertEqual(filtered.addresses, ["192.168.1.20"])
	}

	func testBonjourCandidateRejectsLiteralTailscaleHost() {
		let candidate = Candidate(host: "100.90.0.1", name: "Titan")

		XCTAssertNil(Titand.localDiscoveryCandidate(candidate))
	}

	func testManualDiscoveryAddressAcceptsDirectIPsAndLocalHostnames() {
		XCTAssertEqual(Titand.manualDiscoveryHost(from: "10.0.0.1"), "10.0.0.1")
		XCTAssertEqual(Titand.manualDiscoveryHost(from: "172.16.0.1"), "172.16.0.1")
		XCTAssertEqual(Titand.manualDiscoveryHost(from: " 192.168.1.20\n"), "192.168.1.20")
		XCTAssertEqual(Titand.manualDiscoveryHost(from: "169.254.1.1"), "169.254.1.1")
		XCTAssertEqual(Titand.manualDiscoveryHost(from: "100.90.0.1"), "100.90.0.1")
		XCTAssertEqual(Titand.manualDiscoveryHost(from: " Titan-4.LOCAL. "), "titan-4.local")
	}

	func testManualDiscoveryUsesAddressAsSavedNameFallback() throws {
		let candidate = try XCTUnwrap(Titand.manualDiscoveryCandidate(from: "100.90.0.1"))

		XCTAssertEqual(candidate.name, "100.90.0.1")
	}

	func testManualDiscoveryUsesLocalHostnameDirectly() throws {
		let candidate = try XCTUnwrap(Titand.manualDiscoveryCandidate(from: "titan.local"))

		XCTAssertEqual(candidate.host, "titan.local")
		XCTAssertEqual(candidate.name, "titan.local")
		XCTAssertTrue(candidate.addresses.isEmpty)
	}

	func testManualDiscoveryAcceptsMagicDNSNamesForResolution() {
		XCTAssertEqual(Titand.manualDiscoveryHost(from: "titan"), "titan")
		XCTAssertEqual(
			Titand.manualDiscoveryHost(from: " Titan.My-Tailnet.ts.net. "),
			"titan.my-tailnet.ts.net"
		)
	}

	func testManualDiscoveryAcceptsPlainHTTPAndHTTPSURLs() {
		XCTAssertEqual(Titand.manualDiscoveryHost(from: "http://titan.local/"), "titan.local")
		XCTAssertEqual(Titand.manualDiscoveryHost(from: "HTTPS://Titan/"), "titan")
		XCTAssertEqual(Titand.manualDiscoveryHost(from: "http://192.168.1.20/"), "192.168.1.20")
		XCTAssertEqual(Titand.manualDiscoveryHost(from: "https://100.90.0.1"), "100.90.0.1")
		XCTAssertEqual(
			Titand.manualDiscoveryHost(from: "https://titan.my-tailnet.ts.net/"),
			"titan.my-tailnet.ts.net"
		)
	}

	func testManualDiscoveryUsesOnlyResolvedTailscaleAddressesForMagicDNS() throws {
		let candidate = try XCTUnwrap(Titand.manualDiscoveryCandidate(
			from: "titan.my-tailnet.ts.net",
			resolvedIPv4Addresses: ["192.168.1.20", "100.90.0.1", "203.0.113.2", "100.90.0.2", "100.90.0.1"]
		))

		XCTAssertEqual(candidate.host, "100.90.0.1")
		XCTAssertEqual(candidate.addresses, ["100.90.0.2"])
		XCTAssertEqual(candidate.name, "titan.my-tailnet.ts.net")
	}

	func testManualDiscoveryAllowsSafeLocalAndTailscaleResultsForShortHostnames() throws {
		let candidate = try XCTUnwrap(Titand.manualDiscoveryCandidate(
			from: "titan",
			resolvedIPv4Addresses: ["203.0.113.2", "192.168.1.20", "100.90.0.1", "192.168.1.20"]
		))

		XCTAssertEqual(candidate.host, "192.168.1.20")
		XCTAssertEqual(candidate.addresses, ["100.90.0.1"])
		XCTAssertEqual(candidate.name, "titan")
	}

	func testManualDiscoveryRejectsNonTailscaleDNSResults() {
		XCTAssertNil(Titand.manualDiscoveryCandidate(
			from: "titan.my-tailnet.ts.net",
			resolvedIPv4Addresses: ["192.168.1.20", "203.0.113.2"]
		))
	}

	func testManualDiscoveryAddressRejectsPublicHostnamesAndURLSyntax() {
		XCTAssertNil(Titand.manualDiscoveryHost(from: "example.com"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "https://example.com"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "ftp://titan.local"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "https://user@titan.local"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "https://titan.local:443"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "https://titan.local/settings"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "https://titan.local?tab=apps"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "https://titan.local#apps"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "192.168.1.20:443"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "192.168.1.999"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "bad_name.ts.net"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "0.0.0.0"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "127.0.0.1"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "172.15.255.255"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "172.32.0.0"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "203.0.113.2"))
		XCTAssertNil(Titand.manualDiscoveryHost(from: "224.0.0.1"))
	}

	func testManualDiscoveryReportsUnsupportedLiteralAsInvalid() async {
		do {
			_ = try await Titand.discoverManually(at: "127.0.0.1")
			XCTFail("Expected an invalid-address error")
		} catch {
			XCTAssertEqual(error as? Titand.ManualDiscoveryError, .invalidAddress)
		}
	}

	func testManualDiscoveryReportsUnsupportedResolvedAddressAsNotFound() async {
		do {
			_ = try await Titand.discoverManually(at: "localhost")
			XCTFail("Expected a no-device-found error")
		} catch {
			XCTAssertEqual(error as? Titand.ManualDiscoveryError, .noDeviceFound)
		}
	}

	func testSystemIPv4ResolverUsesLocalDNSConfiguration() async throws {
		let addresses = try await IPv4HostResolver.resolve("localhost")

		XCTAssertTrue(addresses.contains("127.0.0.1"))
	}

	func testLiveManualAddressDiscovery() async throws {
		guard let host = ProcessInfo.processInfo.environment["TITANKIT_MANUAL_DISCOVERY_TEST_HOST"] else {
			throw XCTSkip("Set TITANKIT_MANUAL_DISCOVERY_TEST_HOST to an Titan IP or hostname")
		}

		guard case .device = try await Titand.discoverManually(at: host) else {
			return XCTFail("Expected a current Titan at \(host)")
		}
	}

	func testVerifiedBonjourRenameReplacesStaleHostname() {
		var saved = SavedDevice(
			id: "device",
			name: "Titan",
			host: "titan.local",
			addresses: ["titan.local", "192.168.1.10", "100.90.0.1"]
		)
		let discovered = IdentifiedDevice(
			host: "titan-2.local",
			discoveryHost: "titan-2.local",
			addresses: ["192.168.1.20"],
			name: "Titan 2",
			id: "device",
			model: "Umbrel Home",
			onboarded: true
		)

		saved.mergeVerifiedDiscovery(discovered)

		XCTAssertEqual(saved.host, "titan-2.local")
		XCTAssertFalse(saved.addresses.contains("titan.local"))
		XCTAssertTrue(saved.addresses.contains("192.168.1.10"))
		XCTAssertTrue(saved.addresses.contains("192.168.1.20"))
		XCTAssertTrue(saved.addresses.contains("100.90.0.1"))
		XCTAssertEqual(saved.photoBackupHost, "100.90.0.1")
	}

	func testDiscoveryForAnotherDeviceCannotChangeSavedDevice() {
		var saved = SavedDevice(id: "device", name: "Titan", host: "titan.local", addresses: [])
		let original = saved
		let discovered = IdentifiedDevice(
			host: "titan-2.local",
			discoveryHost: "titan-2.local",
			addresses: ["192.168.1.20"],
			name: "Other Titan",
			id: "other",
			model: "Umbrel Home",
			onboarded: true
		)

		saved.mergeVerifiedDiscovery(discovered)

		XCTAssertEqual(saved, original)
	}

	func testPhotoBackupHostUsesTailscaleAddress() {
		let saved = SavedDevice(
			id: "device",
			name: "Titan",
			host: "100.90.0.1",
			addresses: ["192.168.1.20"]
		)

		XCTAssertEqual(saved.photoBackupHost, "100.90.0.1")
	}

	func testPhotoBackupHostFindsTailscaleAddressAmongCandidates() {
		let saved = SavedDevice(
			id: "device",
			name: "Titan",
			host: "titan.local",
			addresses: ["192.168.1.20", "100.90.0.1"]
		)

		XCTAssertEqual(saved.photoBackupHost, "100.90.0.1")
	}

	func testReportedTailscaleAddressReplacesCanonicalPairingAddressForNewBackups() {
		let saved = SavedDevice(
			id: "device",
			name: "Titan",
			host: "100.90.0.1",
			addresses: ["192.168.1.20", "100.90.0.2"]
		)

		XCTAssertEqual(saved.photoBackupHost, "100.90.0.2")
	}

	func testVerifiedIPAddressReplacesAnUnreachableBonjourHostname() {
		var saved = SavedDevice(
			id: "device",
			name: "Titan",
			host: "titan.local",
			addresses: ["titan.local", "192.168.1.10"]
		)
		let discovered = IdentifiedDevice(
			host: "192.168.1.20",
			discoveryHost: "titan-2.local",
			addresses: ["192.168.1.20"],
			name: "Titan",
			id: "device",
			model: "Umbrel Home",
			onboarded: true
		)

		saved.mergeVerifiedDiscovery(discovered)

		XCTAssertEqual(saved.host, "192.168.1.20")
		XCTAssertFalse(saved.addresses.contains("titan.local"))
		XCTAssertNil(saved.photoBackupHost)
	}
}
