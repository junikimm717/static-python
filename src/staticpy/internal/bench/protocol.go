package bench

// Protocol is the session schema + measurement contract. Bump it when a
// bug is found that means previously recorded numbers cannot be compared
// to new ones (wrong reduction, silently dropped arms, contaminated pin,
// etc), or when the session JSON schema changes in a way that would make
// an old fingerprint_sha256 or interpreter record mean something else.
// A pin bump of pyperformance is a suite change, not a protocol bump.
// Adding a field readers can ignore (suite.name) is not a bump.
const Protocol = 2

// Suite names recorded on every session's suite object. The files on disk
// are the same regardless of which one produced them.
const (
	SuitePyperformance = "pyperformance"
	SuiteMicro         = "micro"
)
