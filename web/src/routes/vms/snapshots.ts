// What GET /vms/<name>/snapshots returns.
export type SnapshotSchedule = { enabled: boolean; time: string; days: number[]; keep: number; memory: boolean; allowPause: boolean };
export type Snapshot = { id: string; name: string; created: string; memory: boolean; automatic: boolean; bytes: number; incomplete?: boolean };
export type SnapshotStatus = {
	snapshots: Snapshot[];
	schedule: SnapshotSchedule;
	pending: boolean;
	reason: '' | 'passthrough' | 'folders' | 'block' | 'disks' | 'preparing' | 'volume' | 'iso';
	job: { state: 'running' | 'failed'; text: string } | null;
};
