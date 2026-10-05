import XCTest
@testable import TitanKit

@MainActor
final class PhotoBackupPreferenceTests: XCTestCase {
	private var defaults: UserDefaults!

	override func setUp() {
		super.setUp()
		defaults = UserDefaults(suiteName: "PhotoBackupPreferenceTests")
		defaults.removePersistentDomain(forName: "PhotoBackupPreferenceTests")
	}

	func testPreferencesAreIsolatedByDeviceAndAccount() {
		let owner = PhotoBackupPreference(includesPhotos: true, allowsCellular: true)
		let member = PhotoBackupPreference(includesVideos: true)
		PhotoBackupPreferenceStore.save(owner, deviceId: "titan", accountId: "0", activate: true, defaults: defaults)
		PhotoBackupPreferenceStore.save(member, deviceId: "titan", accountId: "nate", activate: true, defaults: defaults)

		XCTAssertEqual(PhotoBackupPreferenceStore.preference(deviceId: "titan", accountId: "0", defaults: defaults), owner)
		XCTAssertEqual(PhotoBackupPreferenceStore.preference(deviceId: "titan", accountId: "nate", defaults: defaults), member)
		XCTAssertFalse(PhotoBackupPreferenceStore.isActive(deviceId: "titan", accountId: "0", defaults: defaults))
		XCTAssertTrue(PhotoBackupPreferenceStore.isActive(deviceId: "titan", accountId: "nate", defaults: defaults))
		XCTAssertEqual(
			PhotoBackupPreferenceStore.activeTarget(defaults: defaults),
			PhotoBackupPreferenceTarget(deviceId: "titan", accountId: "nate")
		)
	}

	func testSavingAnInactivePreferenceDoesNotReplaceTheActiveTarget() {
		PhotoBackupPreferenceStore.save(
			PhotoBackupPreference(includesPhotos: true),
			deviceId: "titan",
			accountId: "0",
			activate: true,
			defaults: defaults
		)
		PhotoBackupPreferenceStore.save(
			PhotoBackupPreference(includesVideos: true),
			deviceId: "titan",
			accountId: "nate",
			activate: false,
			defaults: defaults
		)

		XCTAssertTrue(PhotoBackupPreferenceStore.isActive(deviceId: "titan", accountId: "0", defaults: defaults))
		XCTAssertFalse(PhotoBackupPreferenceStore.isActive(deviceId: "titan", accountId: "nate", defaults: defaults))
	}

	func testRemovingDeviceClearsItsPreferencesAndActiveTarget() {
		PhotoBackupPreferenceStore.save(
			PhotoBackupPreference(includesPhotos: true),
			deviceId: "titan",
			accountId: "nate",
			activate: true,
			defaults: defaults
		)

		PhotoBackupPreferenceStore.removeDevice("titan", defaults: defaults)

		XCTAssertEqual(
			PhotoBackupPreferenceStore.preference(deviceId: "titan", accountId: "nate", defaults: defaults),
			PhotoBackupPreference()
		)
		XCTAssertFalse(PhotoBackupPreferenceStore.isActive(deviceId: "titan", accountId: "nate", defaults: defaults))
	}

	func testLiveConfigurationRepairsOnlyTheActiveTarget() {
		let preference = PhotoBackupPreference(includesVideos: true, allowsCellular: true)
		PhotoBackupPreferenceStore.save(
			preference,
			deviceId: "titan",
			accountId: "nate",
			activate: false,
			defaults: defaults
		)

		PhotoBackupPreferenceStore.reconcileActiveTarget(
			deviceId: "titan",
			accountId: "nate",
			defaults: defaults
		)

		XCTAssertTrue(PhotoBackupPreferenceStore.isActive(deviceId: "titan", accountId: "nate", defaults: defaults))
		XCTAssertEqual(
			PhotoBackupPreferenceStore.preference(deviceId: "titan", accountId: "nate", defaults: defaults),
			preference
		)
	}

	func testDisabledPreferenceCannotBeReactivatedByStaleConfiguration() {
		PhotoBackupPreferenceStore.save(
			PhotoBackupPreference(),
			deviceId: "titan",
			accountId: "nate",
			activate: false,
			defaults: defaults
		)

		PhotoBackupPreferenceStore.reconcileActiveTarget(
			deviceId: "titan",
			accountId: "nate",
			defaults: defaults
		)

		XCTAssertFalse(PhotoBackupPreferenceStore.isActive(
			deviceId: "titan",
			accountId: "nate",
			defaults: defaults
		))
	}
}
