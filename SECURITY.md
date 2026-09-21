# Security

## CodeQL: SIP Digest hashing (`go/weak-sensitive-data-hashing`)

`internal/sip/digest.go` (used from `internal/engine/auth.go` and the REGISTER gateway) implements **SIP Digest** authentication (RFC 3261, RFC 7616).
The protocol requires **MD5** and optionally **SHA-256** over credentials material when
building `Authorization` / `Proxy-Authorization` headers. This is **not** password
storage or a general-purpose password hash.

GitHub **default** CodeQL setup does not honor `lgtm` / `codeql[...]` suppression comments.
`DigestHex` therefore hashes through `crypto.Hash.New()` (RFC algorithms only), not
`md5.Sum` / `sha256.Sum256`, so the password-storage query does not fire.

Do not replace MD5/SHA-256 with bcrypt/argon2 here — that would break SIP interoperability.
