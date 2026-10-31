import type { Temperature } from '#lib/api.ts';
import { defaultPlacement, sanitizeLayout, sourceId } from './tiles.ts';

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
	if (show && !tiles.some((tile) => sourceId(tile.id) === 'temperatures'))
		tiles = [...tiles, defaultPlacement('temperatures')];
	if (!show) tiles = tiles.filter((tile) => sourceId(tile.id) !== 'temperatures');
	return {
		...preferences,
		tiles,
		temperatureSensors: temperatureSelection({ temperatureSensors: selected }),
	};
}

/**
 * Sensors a temperature tile shows: its own choice (`items`, sensor IDs) or,
 * without one, the account's selection from Settings › Temperatures. Sensors
 * that disappeared stay in the list with their last known name.
 */
export function tileTemperatures(
	preferences: Record<string, unknown> | null,
	items: string[] | null,
	sensors: readonly Temperature[],
): { id: string; chip: string; label: string; sensor?: Temperature }[] {
	const account = temperatureSelection(preferences);
	const ids = items ?? account.map((choice) => choice.id);
	return ids.map((id) => {
		const sensor = sensors.find((s) => s.id === id);
		const known = account.find((choice) => choice.id === id);
		return {
			id,
			chip: sensor?.chip ?? known?.chip ?? '',
			label: friendlySensor(sensor?.chip ?? known?.chip ?? '', sensor?.label ?? known?.label ?? id),
			sensor,
		};
	});
}

/** Package temperature of the CPU: Intel coretemp, AMD k10temp or zenpower. */
export function cpuTemperature(
	sensors: readonly Temperature[] | undefined,
	chosen = '',
): Temperature | undefined {
	if (!sensors) return undefined;
	if (chosen) return sensors.find((s) => s.id === chosen);
	return sensors.find(
		(s) =>
			(s.chip === 'coretemp' && s.label === 'Package id 0') ||
			((s.chip === 'k10temp' || s.chip === 'zenpower') && s.label === 'Tctl'),
	);
}

/** A plain name for the sensors everyone has; others keep the driver's label. */
export function friendlySensor(chip: string, label: string): string {
	if (
		(chip === 'coretemp' && label === 'Package id 0') ||
		((chip === 'k10temp' || chip === 'zenpower') && label === 'Tctl')
	)
		return 'CPU';
	if (chip === 'nvme' && label === 'Composite') return 'NVMe';
	if (chip === 'amdgpu' && label === 'edge') return 'GPU';
	return label;
}
