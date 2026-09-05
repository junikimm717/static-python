// endians solved!
#define WORDS_BIGENDIAN 1
#undef DOUBLE_IS_LITTLE_ENDIAN_IEEE754
#define DOUBLE_IS_BIG_ENDIAN_IEEE754 1

/* Alignment */
#define HAVE_ALIGNED_REQUIRED 1

/* musl default pthread stack is 128 KiB. s390x eval frames overflow that
   before 3.14's C-stack check (Py_C_STACK_SIZE 320000) can raise
   RecursionError. Same knob CPython already sets for FreeBSD/AIX.
   See staticpy-traps: test_threading.test_recursion_limit SIGSEGV. */
#define THREAD_STACK_SIZE 0x400000
