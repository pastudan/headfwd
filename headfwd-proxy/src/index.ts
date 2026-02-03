/**
 * HeadFwd Proxy - Reverse tunnel for Headscale instances
 *
 * Routes client requests to Headscale instances behind NAT via persistent WebSocket tunnels
 *
 * Authentication: X25519 ECDH + HMAC-SHA256 challenge-response
 * The sidecar proves ownership of the Headscale Noise private key (X25519)
 * by computing a shared secret with the proxy's ephemeral key and HMACing a nonce.
 */

import type { Env } from './types';
import { HeadscaleTunnel } from './tunnel-do';

export { HeadscaleTunnel };

// In-memory storage for local development (when KV is not available)
const localChallenges = new Map<string, { publicKey: string; nonce: string; ephemeralPrivateKeyJwk: JsonWebKey; expiresAt: number }>();
const localRegistrations = new Map<string, { publicKey: string; secret: string }>();

// Helper to cleanup expired challenges on-demand
function cleanupExpiredChallenges() {
	const now = Date.now();
	for (const [fingerprint, data] of localChallenges.entries()) {
		if (data.expiresAt < now) {
			localChallenges.delete(fingerprint);
		}
	}
}

export default {
	async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
		const url = new URL(request.url);

		// Health check
		if (url.pathname === '/health') {
			return new Response('OK', { status: 200 });
		}

		// Registration endpoints (two-phase challenge-response)
		if (url.pathname === '/api/register/init' && request.method === 'POST') {
			return handleRegisterInit(request, env);
		}
		if (url.pathname === '/api/register/verify' && request.method === 'POST') {
			return handleRegisterVerify(request, env);
		}
		// Keep old endpoint for backward compatibility during testing
		if (url.pathname === '/api/register' && request.method === 'POST') {
			return handleRegister(request, env);
		}

		// Local dev: route all other requests through default DO
		const isLocalhost = url.hostname === 'localhost' || url.hostname === '127.0.0.1';
		if (isLocalhost) {
			// For local dev, use 'local-dev' as the DO name
			// In production, this would use the fingerprint from subdomain
			const doId = env.TUNNELS.idFromName('local-dev');
			const doStub = env.TUNNELS.get(doId);
			return doStub.fetch(request);
		}

		// Extract fingerprint from subdomain
		const fingerprint = extractFingerprint(url.hostname);

		if (!fingerprint) {
			// Landing page
			if (url.hostname === 'headfwd.net') {
				return new Response(getLandingPage(), {
					headers: { 'Content-Type': 'text/html' },
				});
			}

			return new Response('Invalid subdomain. Expected: <fingerprint>.headfwd.net', {
				status: 400,
			});
		}

		// Route to Durable Object for this Headscale instance
		const doId = env.TUNNELS.idFromName(fingerprint);
		const doStub = env.TUNNELS.get(doId);

		return doStub.fetch(request);
	},
} satisfies ExportedHandler<Env>;

/**
 * Register a new Headscale instance (legacy - for backward compatibility)
 */
async function handleRegister(request: Request, env: Env): Promise<Response> {
	try {
		const { pubkey } = (await request.json()) as { pubkey: string };

		if (!pubkey) {
			return new Response(JSON.stringify({ error: 'Missing pubkey' }), { status: 400, headers: { 'Content-Type': 'application/json' } });
		}

		// Compute fingerprint
		const fingerprint = await computeFingerprint(pubkey);

		// For local dev, use the configured TUNNEL_SECRET
		const secret = env.TUNNEL_SECRET;

		// Store in KV if available (production), otherwise just return the info (local dev)
		if (env.REGISTRY) {
			await env.REGISTRY.put(
				`tunnel:${fingerprint}`,
				JSON.stringify({
					publicKey: pubkey,
					secret,
					createdAt: Date.now(),
				}),
				{ expirationTtl: 60 * 60 * 24 * 365 } // 1 year
			);
		}

		// For local dev, use localhost
		const hostname = new URL(request.url).hostname;
		const isLocal = hostname === 'localhost' || hostname.startsWith('127.0.0.1');
		const baseUrl = isLocal ? 'localhost:8787' : 'headfwd.net';
		const protocol = isLocal ? 'ws' : 'wss';
		const httpProtocol = isLocal ? 'http' : 'https';

		return new Response(
			JSON.stringify({
				fingerprint,
				tunnelUrl: `${protocol}://${fingerprint}.${baseUrl}/tunnel?auth=${secret}`,
				publicUrl: `${httpProtocol}://${fingerprint}.${baseUrl}`,
			}),
			{ headers: { 'Content-Type': 'application/json' } }
		);
	} catch (error) {
		return new Response(JSON.stringify({ error: 'Invalid request' }), { status: 400, headers: { 'Content-Type': 'application/json' } });
	}
}

/**
 * Phase 1: Initialize registration - generate ephemeral X25519 keypair and challenge nonce
 */
async function handleRegisterInit(request: Request, env: Env): Promise<Response> {
	try {
		const { publicKey } = (await request.json()) as { publicKey: string };

		if (!publicKey) {
			return new Response(JSON.stringify({ error: 'Missing publicKey' }), {
				status: 400,
				headers: { 'Content-Type': 'application/json' },
			});
		}

		// Validate publicKey format (should be mkey:hex)
		if (!publicKey.startsWith('mkey:')) {
			return new Response(JSON.stringify({ error: 'Invalid publicKey format. Expected mkey:hex' }), {
				status: 400,
				headers: { 'Content-Type': 'application/json' },
			});
		}

		// Compute fingerprint from X25519 Noise public key
		const fingerprint = await computeFingerprint(publicKey);

		// Generate challenge nonce (32 bytes = 64 hex chars)
		const nonce = generateNonce();

		// Generate ephemeral X25519 keypair for ECDH
		const ephemeralKeypair = await crypto.subtle.generateKey(
			{ name: 'X25519' },
			true, // extractable (need to store private key for verification)
			['deriveBits']
		) as CryptoKeyPair;

		// Export ephemeral public key (raw bytes) to send to sidecar
		const ephemeralPublicRaw = await crypto.subtle.exportKey('raw', ephemeralKeypair.publicKey) as ArrayBuffer;

		// Export ephemeral private key (JWK) to store for later ECDH
		const ephemeralPrivateKeyJwk = await crypto.subtle.exportKey('jwk', ephemeralKeypair.privateKey) as JsonWebKey;

		// Store challenge with 5-minute expiration
		const expiresAt = Date.now() + 300000;
		if (env.REGISTRY) {
			await env.REGISTRY.put(
				`challenge:${fingerprint}`,
				JSON.stringify({
					publicKey,
					nonce,
					ephemeralPrivateKeyJwk,
					createdAt: Date.now(),
				}),
				{ expirationTtl: 300 } // 5 minutes
			);
		} else {
			localChallenges.set(fingerprint, {
				publicKey,
				nonce,
				ephemeralPrivateKeyJwk,
				expiresAt,
			});
		}

		return new Response(
			JSON.stringify({
				fingerprint,
				nonce,
				proxyPublicKey: bufferToHex(ephemeralPublicRaw),
				expiresAt,
			}),
			{ headers: { 'Content-Type': 'application/json' } }
		);
	} catch (error) {
		return new Response(JSON.stringify({ error: 'Invalid request' }), {
			status: 400,
			headers: { 'Content-Type': 'application/json' },
		});
	}
}

/**
 * Phase 2: Verify HMAC proof via X25519 ECDH shared secret
 *
 * The sidecar computed: HMAC-SHA256(ECDH(sidecarPrivate, proxyEphemeralPublic), nonce)
 * We compute:           HMAC-SHA256(ECDH(proxyEphemeralPrivate, sidecarPublic), nonce)
 * Both shared secrets are identical (ECDH property), so the HMACs must match.
 */
async function handleRegisterVerify(request: Request, env: Env): Promise<Response> {
	try {
		const { fingerprint, proof } = (await request.json()) as { fingerprint: string; proof: string };

		if (!fingerprint || !proof) {
			return new Response(JSON.stringify({ error: 'Missing fingerprint or proof' }), {
				status: 400,
				headers: { 'Content-Type': 'application/json' },
			});
		}

		// Retrieve challenge
		let publicKey: string;
		let nonce: string;
		let ephemeralPrivateKeyJwk: JsonWebKey;

		if (env.REGISTRY) {
			const challengeData = await env.REGISTRY.get(`challenge:${fingerprint}`);
			if (!challengeData) {
				return new Response(JSON.stringify({ error: 'Challenge expired or not found. Please call /api/register/init first.' }), {
					status: 400,
					headers: { 'Content-Type': 'application/json' },
				});
			}
			const parsed = JSON.parse(challengeData);
			publicKey = parsed.publicKey;
			nonce = parsed.nonce;
			ephemeralPrivateKeyJwk = parsed.ephemeralPrivateKeyJwk;
		} else {
			cleanupExpiredChallenges();
			const challenge = localChallenges.get(fingerprint);
			if (!challenge || challenge.expiresAt < Date.now()) {
				localChallenges.delete(fingerprint);
				return new Response(JSON.stringify({ error: 'Challenge expired or not found. Please call /api/register/init first.' }), {
					status: 400,
					headers: { 'Content-Type': 'application/json' },
				});
			}
			publicKey = challenge.publicKey;
			nonce = challenge.nonce;
			ephemeralPrivateKeyJwk = challenge.ephemeralPrivateKeyJwk;
		}

		// Parse sidecar's X25519 public key (remove "mkey:" prefix, decode hex)
		const sidecarPublicKeyBytes = hexToBytes(publicKey.slice(5));

		// Import sidecar's X25519 public key
		const sidecarPublicKey = await crypto.subtle.importKey(
			'raw',
			sidecarPublicKeyBytes,
			{ name: 'X25519' },
			false,
			[]
		);

		// Import proxy's ephemeral private key
		const proxyPrivateKey = await crypto.subtle.importKey(
			'jwk',
			ephemeralPrivateKeyJwk,
			{ name: 'X25519' },
			false,
			['deriveBits']
		);

		// Compute shared secret via X25519 ECDH
		const sharedSecret = await crypto.subtle.deriveBits(
			{ name: 'X25519', $public: sidecarPublicKey } as SubtleCryptoDeriveKeyAlgorithm,
			proxyPrivateKey,
			256 // 32 bytes
		);

		// Compute expected HMAC proof
		const hmacKey = await crypto.subtle.importKey(
			'raw',
			sharedSecret,
			{ name: 'HMAC', hash: 'SHA-256' },
			false,
			['sign']
		);

		const expectedProofBuffer = await crypto.subtle.sign(
			'HMAC',
			hmacKey,
			new TextEncoder().encode(nonce)
		);

		const expectedProof = bufferToHex(expectedProofBuffer);

		// Constant-time comparison to prevent timing attacks
		if (!timingSafeEqual(proof, expectedProof)) {
			return new Response(JSON.stringify({ error: 'Invalid proof' }), {
				status: 403,
				headers: { 'Content-Type': 'application/json' },
			});
		}

		// Authenticated! Generate tunnel secret
		const secret = generateSecret();

		// Store registration
		if (env.REGISTRY) {
			await env.REGISTRY.put(
				`tunnel:${fingerprint}`,
				JSON.stringify({
					publicKey,
					secret,
					createdAt: Date.now(),
				}),
				{ expirationTtl: 60 * 60 * 24 * 365 } // 1 year
			);

			// Clean up challenge
			await env.REGISTRY.delete(`challenge:${fingerprint}`);
		} else {
			localRegistrations.set(fingerprint, { publicKey, secret });
			localChallenges.delete(fingerprint);
		}

		// For local dev, use localhost
		const hostname = new URL(request.url).hostname;
		const isLocal = hostname === 'localhost' || hostname.startsWith('127.0.0.1');
		const baseUrl = isLocal ? 'localhost:8787' : 'headfwd.net';
		const protocol = isLocal ? 'ws' : 'wss';
		const httpProtocol = isLocal ? 'http' : 'https';

		return new Response(
			JSON.stringify({
				fingerprint,
				tunnelUrl: `${protocol}://${fingerprint}.${baseUrl}/tunnel?auth=${secret}`,
				publicUrl: `${httpProtocol}://${fingerprint}.${baseUrl}`,
			}),
			{ headers: { 'Content-Type': 'application/json' } }
		);
	} catch (error) {
		return new Response(JSON.stringify({ error: 'Invalid request' }), {
			status: 400,
			headers: { 'Content-Type': 'application/json' },
		});
	}
}

/**
 * Extract fingerprint from subdomain
 */
function extractFingerprint(hostname: string): string | null {
	const match = hostname.match(/^([a-f0-9]{32})\.headfwd\.net$/i);
	return match ? match[1].toLowerCase() : null;
}

/**
 * Compute SHA256 fingerprint from public key (hex32 - first 32 chars / 128 bits)
 */
async function computeFingerprint(pubkey: string): Promise<string> {
	const encoder = new TextEncoder();
	const data = encoder.encode(pubkey);
	const hashBuffer = await crypto.subtle.digest('SHA-256', data);
	const hashArray = Array.from(new Uint8Array(hashBuffer));
	const fullHex = hashArray.map((b) => b.toString(16).padStart(2, '0')).join('');
	// Use first 32 chars (128 bits) for subdomain
	return fullHex.substring(0, 32);
}

/**
 * Generate secure random secret
 */
function generateSecret(): string {
	const buffer = new Uint8Array(32);
	crypto.getRandomValues(buffer);
	return Array.from(buffer)
		.map((b) => b.toString(16).padStart(2, '0'))
		.join('');
}

/**
 * Generate challenge nonce (32 bytes)
 */
function generateNonce(): string {
	const buffer = new Uint8Array(32);
	crypto.getRandomValues(buffer);
	return Array.from(buffer)
		.map((b) => b.toString(16).padStart(2, '0'))
		.join('');
}

/**
 * Constant-time string comparison to prevent timing attacks
 */
function timingSafeEqual(a: string, b: string): boolean {
	if (a.length !== b.length) return false;
	let result = 0;
	for (let i = 0; i < a.length; i++) {
		result |= a.charCodeAt(i) ^ b.charCodeAt(i);
	}
	return result === 0;
}

/**
 * Convert ArrayBuffer to hex string
 */
function bufferToHex(buffer: ArrayBuffer): string {
	return Array.from(new Uint8Array(buffer))
		.map((b) => b.toString(16).padStart(2, '0'))
		.join('');
}

/**
 * Convert hex string to Uint8Array
 */
function hexToBytes(hex: string): Uint8Array {
	const bytes = new Uint8Array(hex.length / 2);
	for (let i = 0; i < hex.length; i += 2) {
		bytes[i / 2] = parseInt(hex.substr(i, 2), 16);
	}
	return bytes;
}

/**
 * Landing page
 */
function getLandingPage(): string {
	return `
<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <title>HeadFwd - Decentralized Remote Access</title>
  <style>
    body {
      font-family: -apple-system, sans-serif;
      background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
      color: white;
      height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      margin: 0;
      padding: 20px;
    }
    .container { max-width: 600px; text-align: center; }
    h1 { font-size: 3rem; margin-bottom: 1rem; }
    p { font-size: 1.2rem; line-height: 1.6; opacity: 0.9; }
  </style>
</head>
<body>
  <div class="container">
    <h1>🔐 HeadFwd</h1>
    <p>Reverse tunnel proxy for self-hosted Headscale instances</p>
    <p>Access your mesh network from anywhere, even behind NAT</p>
  </div>
</body>
</html>
  `.trim();
}
