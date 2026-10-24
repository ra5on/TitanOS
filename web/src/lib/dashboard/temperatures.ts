import { defaultPlacement, sanitizeLayout } from './tiles.ts';

export type SelectedTemperature = { id: string; chip: string; label: string };

export function temperatureSelection(
	preferences: Record<string, unknown> | null,
): SelectedTemperature[] {
	const values = preferences?.temperatureSensors;
	if (!Array.isArray(values)) return [];
	const seen = new Set<string>();
	return values
		.filter((value): value is SelectedTemperature => {
			if (
				!value ||
				typeof value !== 'object' ||
				typeof value.id !== 'string' ||
				!/^[a-f0-9]{32}$/.test(value.id) ||
				seen.has(value.id)
			)
				return false;
			if (
				typeof value.chip !== 'string' ||
				typeof value.label !== 'string' ||
				value.chip.length > 128 ||
				value.label.length > 128
			)
				return false;
			seen.add(value.id);
			return true;
		})
		.slice(0, 128)
		.map(({ id, chip, label }) => ({ id, chip, label }));
}

export function withTemperatures(
	preferences: Record<string, unknown> | null,
	show: boolean,
	selected: SelectedTemperature[],
) {
	let tiles = sanitizeLayout(preferences);
	if (show && !tiles.some((tile) => tile.id === 'temperatures'))
		tiles = [...tiles, defaultPlacement('temperatures')];
	if (!show) tiles = tiles.filter((tile) => tile.id !== 'temperatures');
	return {
		...preferences,
		tiles,
		temperatureSensors: temperatureSelection({ temperatureSensors: selected }),
	};
}
