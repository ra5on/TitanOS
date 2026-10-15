import type { TileId } from './tiles.ts';
export type TileOptions = {
 details: boolean; history: boolean; temperature: boolean;
 ipv4: boolean; ipv6: boolean; link: boolean; rates: boolean; interface: string;
};
export function tileOptions(input: unknown, id: TileId): TileOptions {
 const raw = (input as Record<string, unknown> | null)?.[id];
 const value = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
 const bool = (key: string, fallback: boolean) => typeof value[key] === 'boolean' ? value[key] as boolean : fallback;
 return { details: bool('details', !['cpu', 'memory'].includes(id)), history: bool('history', false), temperature: bool('temperature', true),
  ipv4: bool('ipv4', true), ipv6: bool('ipv6', true), link: bool('link', true), rates: bool('rates', true),
  interface: typeof value.interface === 'string' && /^[a-zA-Z0-9_.:@-]{1,15}$/.test(value.interface) ? value.interface : '' };
}
