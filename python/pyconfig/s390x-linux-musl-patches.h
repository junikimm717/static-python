/* Define if we can use x64 gcc inline assembler */
#undef HAVE_GCC_ASM_FOR_X64

/* Define if we can use gcc inline assembler to get and set x87 control word
   */
#undef HAVE_GCC_ASM_FOR_X87

// endians solved!
#define WORDS_BIGENDIAN 1
#undef DOUBLE_IS_LITTLE_ENDIAN_IEEE754
#define DOUBLE_IS_BIG_ENDIAN_IEEE754 1

/* Alignment */
#define HAVE_ALIGNED_REQUIRED 1
