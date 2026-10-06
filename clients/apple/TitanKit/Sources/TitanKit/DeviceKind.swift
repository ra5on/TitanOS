import Foundation

/// The hardware family represented by titand's human-readable device model.
///
/// Both Apple apps use this small shared classifier so a newly discovered or
/// offline device cannot render as a different Titan model by accident.
public enum TitanDeviceKind: Sendable, Equatable {
	case home
	case pro
	case raspberryPi
	case generic

	public init(model: String?) {
		let model = model?.lowercased() ?? ""
		if model.contains("umbrel home") || model.contains("titan home") {
			self = .home
		} else if model.contains("umbrel pro") || model.contains("titan pro") {
			self = .pro
		} else if model.contains("raspberry pi") {
			self = .raspberryPi
		} else {
			self = .generic
		}
	}
}
