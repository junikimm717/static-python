/* Define if we can use x64 gcc inline assembler */
#undef HAVE_GCC_ASM_FOR_X64

/* Define if we can use gcc inline assembler to get and set x87 control word
   */
#undef HAVE_GCC_ASM_FOR_X87

// so...it turns out that endian stuff is getting fked so I can't actually use powerpc64 for now :)
#define WORDS_BIGENDIAN 1

/* Alignment */
#define HAVE_ALIGNED_REQUIRED 1
