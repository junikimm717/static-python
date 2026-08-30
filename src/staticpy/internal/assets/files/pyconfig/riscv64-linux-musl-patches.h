#define HAVE_ALIGNED_REQUIRED 1
// Bruh why does the musl toolchain not have libatomic.a bundled with gcc :/
#undef HAVE___BUILTIN_CLZ
#undef HAVE_BUILTIN_ATOMIC
