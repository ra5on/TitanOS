// Passkeys (WebAuthn) in the browser: turns the options of the NAS into
// what navigator.credentials expects and the answers back into base64url.
import { api } from './api.ts';

function fromB64(value: string): ArrayBuffer {
	const base64 = value.replace(/-/g, '+').replace(/_/g, '/');
	const binary = atob(base64 + '='.repeat((4 - (base64.length % 4)) % 4));
	const bytes = new Uint8Array(binary.length);
	for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
	return bytes.buffer;
}

export function toB64(buffer: ArrayBuffer | null | undefined): string {
	if (!buffer) return '';
	let binary = '';
	for (const byte of new Uint8Array(buffer)) binary += String.fromCharCode(byte);
	return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

/** Whether this page can use passkeys at all: a browser with WebAuthn and a host name (not an IP address). */
export function passkeysPossible(): boolean {
	if (typeof window === 'undefined' || !window.PublicKeyCredential || !window.isSecureContext) return false;
	const host = window.location.hostname;
	return !/^[\d.]+$/.test(host) && !host.includes(':') && !host.startsWith('[');
}

type CreateOptions = {
	challengeId: string;
	publicKey: {
		challenge: string;
		rp: { id: string; name: string };
		user: { id: string; name: string; displayName: string };
		pubKeyCredParams: { type: 'public-key'; alg: number }[];
		timeout: number;
		excludeCredentials: { type: 'public-key'; id: string }[];
		attestation: AttestationConveyancePreference;
		authenticatorSelection: AuthenticatorSelectionCriteria;
	};
};

export type Passkey = { id: string; name: string; rpId: string; synced: boolean; created: string };

/** Registers a new passkey for the logged-in account; password (and code) prove it is the owner. */
export async function addPasskey(password: string, code: string, name: string): Promise<Passkey[]> {
	const options = await api.post<CreateOptions>('/account/passkeys/begin', { password, code });
	const p = options.publicKey;
	const credential = (await navigator.credentials.create({
		publicKey: {
			...p,
			challenge: fromB64(p.challenge),
			user: { ...p.user, id: fromB64(p.user.id) },
			excludeCredentials: p.excludeCredentials.map((c) => ({ ...c, id: fromB64(c.id) }))
		}
	})) as PublicKeyCredential | null;
	if (!credential) throw new Error('cancelled');
	const response = credential.response as AuthenticatorAttestationResponse;
	const answer = await api.post<{ passkeys: Passkey[] }>('/account/passkeys', {
		challengeId: options.challengeId,
		name,
		clientDataJSON: toB64(response.clientDataJSON),
		attestationObject: toB64(response.attestationObject)
	});
	return answer.passkeys;
}

type GetOptions = {
	challengeId: string;
	publicKey: { challenge: string; rpId: string; timeout: number; userVerification: UserVerificationRequirement };
};

/** Logs in with a passkey the browser or phone holds. */
export async function loginWithPasskey(): Promise<void> {
	const options = await api.post<GetOptions>('/login/passkey/begin');
	const p = options.publicKey;
	const credential = (await navigator.credentials.get({
		publicKey: { ...p, challenge: fromB64(p.challenge), allowCredentials: [] }
	})) as PublicKeyCredential | null;
	if (!credential) throw new Error('cancelled');
	const response = credential.response as AuthenticatorAssertionResponse;
	await api.post('/login/passkey', {
		challengeId: options.challengeId,
		id: toB64(credential.rawId),
		clientDataJSON: toB64(response.clientDataJSON),
		authenticatorData: toB64(response.authenticatorData),
		signature: toB64(response.signature),
		userHandle: toB64(response.userHandle)
	});
}
