#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <sys/types.h>
#include <wchar.h>

int main() {
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
  printf("// %d-bit\n", (int)(sizeof(void*) * 8));

  // gcc stupid int128
#ifdef __SIZEOF_INT128__
  printf("#define HAVE_GCC_UINT128_T 1\n");
#else
  printf("#undef HAVE_GCC_UINT128_T\n");
#endif

  return 0;
}
