package bench

// Protocol is the session schema + measurement contract. Bump it when a
// bug is found that means previously recorded numbers cannot be compared
// to new ones (wrong reduction, silently dropped arms, contaminated pin,
// etc). A pin bump of pyperformance is a suite change, not a protocol bump.
const Protocol = 1
