# SSH Fingerprint Generation for DNS-Safe Subdomains

This document describes the approach for generating DNS-safe fingerprints from SSH public keys for use in headfwd routing.

## Design Decisions

- **Hash Length**: Uses first 32 hex characters of SHA256 (128 bits) instead of full hash
- **Security**: 128 bits provides ~2^64 birthday attack resistance - more than sufficient for routing purposes
- **Encoding**: Hex encoding is trivially reproducible on all platforms (Node, iOS, Android)

## Why Hex Encoding?

We chose hex over other encoding schemes for specific reasons:

- **Base64**: Uses `+`, `/`, `=` and uppercase - not DNS-safe
- **Base32**: Not built into Node.js, would require additional library
- **Base36**: Needs BigInt handling, tricky on iOS (no native BigInt support)
- **Hex**: Built-in everywhere, simple implementation: `sha256(data).hex().slice(0, 32)`

## QR Code Optimization

- Shorter subdomain = smaller QR code = better scanning UX
- **Tip**: Encode URL with UPPERCASE hex in QR data to enable alphanumeric mode (more efficient encoding)
- DNS is case-insensitive so uppercase hex still works perfectly

## Cross-Platform Implementation

### Node.js
```javascript
const crypto = require("node:crypto");

function sshFingerprint(pubKeyLine) {
  const keyBlob = Buffer.from(pubKeyLine.split(/\s+/)[1], "base64");
  return crypto.createHash("sha256").update(keyBlob).digest("hex").slice(0, 32);
}
```

### Android/Java
```java
MessageDigest digest = MessageDigest.getInstance("SHA-256");
byte[] hash = digest.digest(keyBlob);
String fingerprint = bytesToHex(hash).substring(0, 32);
```

### iOS/Swift
```swift
import CryptoKit

let hash = SHA256.hash(data: keyBlob)
let fingerprint = hash.compactMap { String(format: "%02x", $0) }.prefix(32).joined()
```

## Reference Implementation

A complete Node.js reference implementation that processes all SSH public keys in `~/.ssh/`:

```javascript
const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");

function sshFingerprint(pubKeyLine) {
  const keyBlob = Buffer.from(pubKeyLine.split(/\s+/)[1], "base64");
  return crypto.createHash("sha256").update(keyBlob).digest("hex").slice(0, 32);
}

// Get SSH directory
const sshDir = path.join(os.homedir(), ".ssh");

// Read all files in the SSH directory
const files = fs.readdirSync(sshDir);

// Filter for .pub files
const pubKeyFiles = files.filter((file) => file.endsWith(".pub"));

console.log(`Found ${pubKeyFiles.length} public key(s) in ${sshDir}\n`);

// Process each public key file
pubKeyFiles.forEach((file) => {
  const filePath = path.join(sshDir, file);
  try {
    const content = fs.readFileSync(filePath, "utf8").trim();
    const fingerprint = sshFingerprint(content);
    console.log(`${file}:`);
    console.log(`  ${fingerprint}\n`);
  } catch (error) {
    console.log(`${file}:`);
    console.log(`  Error: ${error.message}\n`);
  }
});
```

## Usage

The fingerprint is used to create DNS-safe subdomains for routing:

```
https://<fingerprint>.headfwd.example.com
```

This allows the proxy to route connections based on the client's SSH public key without requiring pre-registration or a database lookup.
