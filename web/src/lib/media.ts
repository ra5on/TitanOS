import { api } from './api.ts';
export type MediaJob = {
	id: string;
	volume: string;
	name: string;
	url?: string;
	uploadName?: string;
	uploadSize?: number;
	phase: string;
	bytes: number;
	total: number;
	written: number;
	outputBytes: number;
	output?: string;
	error?: string;
	sha256?: string;
	sourceReady?: boolean;
};
export const mediaActive = (j: MediaJob) =>
	['downloading', 'extracting', 'verifying', 'publishing', 'pausing'].includes(j.phase);
export async function verifyMediaUpload(
	job: MediaJob,
	file: File,
	chunk: number,
	onprogress: (done: number) => void,
) {
	let proofs: { offset: number; size: number; sha256: string }[] = [];
	for (let offset = 0; offset < job.bytes; offset += chunk) {
		const size = Math.min(chunk, job.bytes - offset);
		const hash = await crypto.subtle.digest(
			'SHA-256',
			await file.slice(offset, offset + size).arrayBuffer(),
		);
		proofs.push({
			offset,
			size,
			sha256: Array.from(new Uint8Array(hash), (b) => b.toString(16).padStart(2, '0')).join(''),
		});
		if (proofs.length === 32 || offset + size === job.bytes) {
			await api.post(`/vms/imports/${job.id}/verify`, { chunks: proofs });
			proofs = [];
			onprogress(offset + size);
		}
	}
}
