/* Define if we can use x64 gcc inline assembler */
#undef HAVE_GCC_ASM_FOR_X64

/* Define if we can use gcc inline assembler to get and set x87 control word
   */
#undef HAVE_GCC_ASM_FOR_X87

#define HAVE_ALIGNED_REQUIRED 1
// Bruh why does the musl toolchain not have libatomic.a bundled with gcc :/
#undef HAVE___BUILTIN_CLZ
#undef HAVE_BUILTIN_ATOMIC
