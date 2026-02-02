/**
 * HeadFwd Proxy - Reverse tunnel for Headscale instances
 *
 * Routes client requests to Headscale instances behind NAT via persistent WebSocket tunnels
 */

import type { Env } from './types';
import { HeadscaleTunnel } from './tunnel-do';
import { ed25519 } from '@noble/curves/ed25519.js';

export { HeadscaleTunnel };

// In-memory storage for local development (when KV is not available)
const localChallenges = new Map<string, { publicKey: string; nonce: string; expiresAt: number }>();
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
 * Phase 1: Initialize registration - get challenge nonce
 */
async function handleRegisterInit(request: Request, env: Env): Promise<Response> {
	try {
		const { publicKey, ed25519PublicKey } = (await request.json()) as { 
			publicKey: string; 
			ed25519PublicKey?: string;
		};

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

		// Compute fingerprint from Curve25519 Noise public key
		const fingerprint = await computeFingerprint(publicKey);

		// Generate challenge nonce (32 bytes = 64 hex chars)
		const nonce = generateNonce();

		// Store challenge with 5-minute expiration
		// Include both Curve25519 key (for fingerprint) and Ed25519 key (for verification)
		const expiresAt = Date.now() + 300000;
		if (env.REGISTRY) {
			await env.REGISTRY.put(
				`challenge:${fingerprint}`,
				JSON.stringify({
					publicKey,
					ed25519PublicKey: ed25519PublicKey || publicKey, // Use Ed25519 key if provided
					nonce,
					createdAt: Date.now(),
				}),
				{ expirationTtl: 300 } // 5 minutes
			);
		} else {
			// Use in-memory storage for local dev
			localChallenges.set(fingerprint, { 
				publicKey: ed25519PublicKey || publicKey, // Store Ed25519 key for verification
				nonce, 
				expiresAt 
			});
		}

		return new Response(
			JSON.stringify({
				fingerprint,
				nonce,
				expiresAt: Date.now() + 300000, // 5 minutes from now
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
 * Phase 2: Verify signature and complete registration
 */
async function handleRegisterVerify(request: Request, env: Env): Promise<Response> {
	try {
		const { fingerprint, signature } = (await request.json()) as { fingerprint: string; signature: string };

		if (!fingerprint || !signature) {
			return new Response(JSON.stringify({ error: 'Missing fingerprint or signature' }), {
				status: 400,
				headers: { 'Content-Type': 'application/json' },
			});
		}

		// Retrieve challenge
		let publicKey: string;
		let nonce: string;
		
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
		} else {
			// Use in-memory storage for local dev
			cleanupExpiredChallenges(); // Clean up expired challenges
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
		}

		// Verify signature using Noise public key
		const isValid = await verifyNoiseSignature(publicKey, nonce, signature);
		if (!isValid) {
			return new Response(JSON.stringify({ error: 'Invalid signature' }), {
				status: 403,
				headers: { 'Content-Type': 'application/json' },
			});
		}

		// Generate tunnel secret
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
			// Use in-memory storage for local dev
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
 * Verify Ed25519 signature
 * 
 * The sidecar converts the Curve25519 Noise private key to Ed25519 for signing.
 * We receive the Ed25519 public key directly and use it for verification.
 */
async function verifyNoiseSignature(publicKeyStr: string, nonce: string, signatureHex: string): Promise<boolean> {
	try {
		// Parse Ed25519 public key (hex string, no prefix)
		let ed25519PublicKey: Uint8Array;
		
		if (publicKeyStr.startsWith('mkey:')) {
			// Legacy: Curve25519 key - won't work for verification
			console.error('Received Curve25519 key instead of Ed25519 key');
			return false;
		} else {
			// Ed25519 public key in hex format
			ed25519PublicKey = hexToBytes(publicKeyStr);
		}

		// Verify signature using @noble/curves
		const nonceBytes = new TextEncoder().encode(nonce);
		const signatureBytes = hexToBytes(signatureHex);

		return ed25519.verify(signatureBytes, nonceBytes, ed25519PublicKey);
	} catch (error) {
		console.error('Signature verification failed:', error);
		return false;
	}
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
