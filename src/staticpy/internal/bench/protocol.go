package bench

// The session schema + measurement contract. Bump it when a bug means
// previously recorded numbers cannot be compared to new ones (wrong reduction,
// silently dropped arms, contaminated pin), or when a schema change would make
// an old fingerprint_sha256 or interpreter record mean something else. A
// pyperformance pin bump or a field readers can ignore is not a bump.
const Protocol = 2

// The files on disk are the same regardless of which suite produced them.
const (
	SuitePyperformance = "pyperformance"
	SuiteMicro         = "micro"
)
