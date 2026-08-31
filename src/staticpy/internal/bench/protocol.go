package bench

// Protocol is the session schema + measurement contract. Bump it when a
// bug is found that means previously recorded numbers cannot be compared
// to new ones (wrong reduction, silently dropped arms, contaminated pin,
// etc), or when the session JSON schema changes in a way that would make
// an old fingerprint_sha256 or interpreter record mean something else.
// A pin bump of pyperformance is a suite change, not a protocol bump.
const Protocol = 2
