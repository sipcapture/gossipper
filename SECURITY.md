# Security

## CodeQL: SIP Digest hashing (`go/weak-sensitive-data-hashing`)

`internal/engine/auth.go` implements **SIP Digest** authentication (RFC 3261, RFC 7616).
The protocol requires **MD5** and optionally **SHA-256** over credentials material when
building `Authorization` / `Proxy-Authorization` headers. This is **not** password
storage or a general-purpose password hash.

CodeQL rule `go/weak-sensitive-data-hashing` reports false positives on `md5Hex` /
`sha256Hex`. Those call sites are annotated with `// codeql[go/weak-sensitive-data-hashing]`
and `// lgtm[go/weak-sensitive-data-hashing]` on the line immediately before the hash.

Do not replace MD5/SHA-256 with bcrypt/argon2 here — that would break SIP interoperability.
