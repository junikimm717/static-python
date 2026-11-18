/* Applies to every target, ahead of <triple>-patches.h. */

/* musl's default thread stack is 128 KiB, but ceval guesses Py_C_STACK_SIZE
   (up to 4 MB) on musl, so recursion guards in threads trust stack that isn't
   there. See staticpy-traps: test_threading.test_recursion_limit SIGSEGV. */
#define THREAD_STACK_SIZE 0x400000
