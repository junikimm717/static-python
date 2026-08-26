// Emits the pyconfig.h facts that CPython's configure determines by *running* a
// program, which is exactly what it cannot do when cross-compiling. Built for
// the target and run under qemu; stdout becomes a config.site and a pyconfig
// fragment.
//
// Everything here must be a runtime or compile-time property of the target
// alone. Deliberate deviations from what the hardware reports (riscv's
// HAVE_BUILTIN_ATOMIC override, say) belong in the per-target fragment, which
// wins over this output.

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>
#include <wchar.h>

// The bit pattern of this double spells "noonsees" when stored big-endian and
// "seesnoon" little-endian. It is the constant autoconf's float-endianness
// macro uses, kept so our answer and configure's cannot disagree.
static const double float_sentinel = 9.090423496703681e+223;

static void sizes(void) {
  printf("#define SIZEOF_INT %zu\n", sizeof(int));
  printf("#define SIZEOF_LONG %zu\n", sizeof(long));
  printf("#define SIZEOF_LONG_LONG %zu\n", sizeof(long long));
  printf("#define SIZEOF_VOID_P %zu\n", sizeof(void *));
  printf("#define SIZEOF_SHORT %zu\n", sizeof(short));
  printf("#define SIZEOF_FLOAT %zu\n", sizeof(float));
  printf("#define SIZEOF_DOUBLE %zu\n", sizeof(double));
  printf("#define SIZEOF_LONG_DOUBLE %zu\n", sizeof(long double));
  printf("#define SIZEOF_FPOS_T %zu\n", sizeof(fpos_t));
  printf("#define SIZEOF_SIZE_T %zu\n", sizeof(size_t));
  printf("#define SIZEOF_SSIZE_T %zu\n", sizeof(ssize_t));
  printf("#define SIZEOF_PID_T %zu\n", sizeof(pid_t));
  printf("#define SIZEOF_UINTPTR_T %zu\n", sizeof(uintptr_t));
  printf("#define SIZEOF_TIME_T %zu\n", sizeof(time_t));
  printf("#define SIZEOF_WCHAR_T %zu\n", sizeof(wchar_t));
  printf("#define SIZEOF__BOOL %zu\n", sizeof(_Bool));
  printf("#define SIZEOF_OFF_T %zu\n", sizeof(off_t));

  printf("#define ALIGNOF_INT %zu\n", __alignof__(int));
  printf("#define ALIGNOF_LONG %zu\n", __alignof__(long));
  printf("#define ALIGNOF_LONG_LONG %zu\n", __alignof__(long long));
  printf("#define ALIGNOF_VOID_P %zu\n", __alignof__(void *));
  printf("#define ALIGNOF_FLOAT %zu\n", __alignof__(float));
  printf("#define ALIGNOF_DOUBLE %zu\n", __alignof__(double));
  printf("#define ALIGNOF_LONG_DOUBLE %zu\n", __alignof__(long double));
  printf("#define ALIGNOF_SIZE_T %zu\n", __alignof__(size_t));
  printf("#define ALIGNOF_WCHAR_T %zu\n", __alignof__(wchar_t));
  printf("#define ALIGNOF__BOOL %zu\n", __alignof__(_Bool));
  printf("// %d-bit\n", (int)(sizeof(void *) * 8));
}

static void word_endianness(void) {
  const uint32_t probe = 0x01020304u;
  unsigned char b[4];
  memcpy(b, &probe, sizeof b);
  if (b[0] == 0x01) {
    printf("#define WORDS_BIGENDIAN 1\n");
  } else {
    printf("#undef WORDS_BIGENDIAN\n");
  }
}

// configure derives DOUBLE_IS_* from the byte order of a stored double, which
// need not match the integer byte order: the mixed-endian case exists precisely
// because some ARM FPUs disagreed with their CPU. Report only what we can see,
// and say so loudly otherwise rather than guessing a third case into existence.
static void double_endianness(void) {
  unsigned char b[sizeof(double)];
  memcpy(b, &float_sentinel, sizeof b);

  if (sizeof(double) == 8 && memcmp(b, "noonsees", 8) == 0) {
    printf("#undef DOUBLE_IS_LITTLE_ENDIAN_IEEE754\n");
    printf("#define DOUBLE_IS_BIG_ENDIAN_IEEE754 1\n");
    return;
  }
  if (sizeof(double) == 8 && memcmp(b, "seesnoon", 8) == 0) {
    printf("#undef DOUBLE_IS_BIG_ENDIAN_IEEE754\n");
    printf("#define DOUBLE_IS_LITTLE_ENDIAN_IEEE754 1\n");
    return;
  }
  printf("#error patcher: unrecognised double byte order:");
  for (size_t i = 0; i < sizeof b; i++) {
    printf(" %02x", b[i]);
  }
  printf(" (mixed-endian? set DOUBLE_IS_* in the per-target fragment)\n");
}

// Upstream's test, run in a child because the misaligned load is allowed to
// fault: on a strict-alignment target that SIGBUS *is* the answer, and taking
// it in-process would lose every line printed above.
static void aligned_required(void) {
  fflush(stdout);
  pid_t pid = fork();
  if (pid == 0) {
    // volatile, unlike upstream's conftest: we build at -O3 -flto, where the
    // compiler can see the whole array and fold the comparison without ever
    // emitting the misaligned load the test exists to attempt.
    static volatile char s[16];
    volatile int *p1, *p2;
    for (int i = 0; i < 16; i++) {
      s[i] = (char)i;
    }
    p1 = (volatile int *)(s + 1);
    p2 = (volatile int *)(s + 2);
    _exit(*p1 == *p2 ? 1 : 0);
  }
  if (pid < 0) {
    printf("#error patcher: fork failed, cannot probe HAVE_ALIGNED_REQUIRED\n");
    return;
  }
  int st = 0;
  if (waitpid(pid, &st, 0) < 0) {
    printf("#error patcher: waitpid failed, cannot probe HAVE_ALIGNED_REQUIRED\n");
    return;
  }
  // Anything but a clean zero exit means the unaligned access did not work.
  // configure's own cross fallback is "required", so erring that way on a
  // surprise keeps us no worse than not probing at all.
  if (WIFEXITED(st) && WEXITSTATUS(st) == 0) {
    printf("#undef HAVE_ALIGNED_REQUIRED\n");
  } else {
    printf("#define HAVE_ALIGNED_REQUIRED 1\n");
  }
}

static void compile_time(void) {
#ifdef __SIZEOF_INT128__
  printf("#define HAVE_GCC_UINT128_T 1\n");
#else
  printf("#undef HAVE_GCC_UINT128_T\n");
#endif

#if defined(__x86_64__) && !defined(__ILP32__)
  printf("#define HAVE_GCC_ASM_FOR_X64 1\n");
#else
  printf("#undef HAVE_GCC_ASM_FOR_X64\n");
#endif

#if defined(__i386__) || defined(__x86_64__)
  printf("#define HAVE_GCC_ASM_FOR_X87 1\n");
#else
  printf("#undef HAVE_GCC_ASM_FOR_X87\n");
#endif
}

int main(void) {
  sizes();
  compile_time();
  word_endianness();
  double_endianness();
  aligned_required();
  return 0;
}
