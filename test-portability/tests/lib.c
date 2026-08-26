#include <stddef.h>

int dot_product(const int *a, const int *b, size_t n) {
	int acc = 0;
	for (size_t i = 0; i < n; i++) {
		acc += a[i] * b[i];
	}
	return acc;
}
