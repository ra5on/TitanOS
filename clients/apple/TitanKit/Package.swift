// swift-tools-version: 5.9
import PackageDescription

// Shared foundation for the Apple client apps: device discovery, the titand API
// client, auth, and saved-device persistence.
let package = Package(
	name: "TitanKit",
	platforms: [.macOS(.v14), .iOS(.v17)],
	products: [
		.library(name: "TitanKit", targets: ["TitanKit"])
	],
	targets: [
		.target(name: "TitanKit"),
		.testTarget(name: "TitanKitTests", dependencies: ["TitanKit"])
	]
)
