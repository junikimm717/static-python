/* Define if we can use x64 gcc inline assembler */
#undef HAVE_GCC_ASM_FOR_X64

/* Define if we can use gcc inline assembler to get and set x87 control word
   */
#undef HAVE_GCC_ASM_FOR_X87
#define SIZEOF_INT 4
#define SIZEOF_LONG 4
#define SIZEOF_LONG_LONG 8
#define SIZEOF_VOID_P 4
#define SIZEOF_SHORT 2
#define SIZEOF_FLOAT 4
#define SIZEOF_DOUBLE 8
#define SIZEOF_LONG_DOUBLE 12
#define SIZEOF_FPOS_T 16
#define SIZEOF_SIZE_T 4
#define SIZEOF_SSIZE_T 4
#define SIZEOF_PID_T 4
#define SIZEOF_UINTPTR_T 4
#define SIZEOF_TIME_T 8
#define SIZEOF_WCHAR_T 4
#define SIZEOF__BOOL 1
#define SIZEOF_OFF_T 8
#define ALIGNOF_INT 4
#define ALIGNOF_LONG 4
#define ALIGNOF_LONG_LONG 8
#define ALIGNOF_VOID_P 4
#define ALIGNOF_FLOAT 4
#define ALIGNOF_DOUBLE 8
#define ALIGNOF_LONG_DOUBLE 4
#define ALIGNOF_SIZE_T 4
#define ALIGNOF_WCHAR_T 4
#define ALIGNOF__BOOL 1
// 32-bit
#undef HAVE_GCC_UINT128_T
