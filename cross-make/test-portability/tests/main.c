#include <stdio.h>

#include "lib.h"

int main(void) {
	int a[] = {1, 2, 3, 4, 5};
	int b[] = {5, 4, 3, 2, 1};
	int r = dot_product(a, b, 5);
	printf("lto-main dot=%d\n", r);
	return (r == 35) ? 0 : 2;
}
