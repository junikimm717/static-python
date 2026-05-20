#include <stdio.h>
#include <stdlib.h>
#include <string.h>

struct node {
	int value;
	struct node *next;
};

typedef int (*reducer)(int acc, int x);

static int sum(int a, int b) { return a + b; }
static int mul(int a, int b) { return a * b; }

static int reduce(struct node *head, int init, reducer fn) {
	int acc = init;
	for (struct node *n = head; n; n = n->next) {
		acc = fn(acc, n->value);
	}
	return acc;
}

int main(void) {
	struct node *head = NULL;
	for (int i = 1; i <= 5; i++) {
		struct node *n = malloc(sizeof(*n));
		if (!n) return 1;
		n->value = i;
		n->next = head;
		head = n;
	}

	int s = reduce(head, 0, sum);
	int p = reduce(head, 1, mul);

	printf("c-hello sum=%d product=%d\n", s, p);

	for (struct node *n = head; n; ) {
		struct node *next = n->next;
		free(n);
		n = next;
	}
	return (s == 15 && p == 120) ? 0 : 2;
}
