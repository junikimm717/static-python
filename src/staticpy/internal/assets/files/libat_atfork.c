/* Drop-in replacement for gcc libatomic config/posix/lock.c that
 * registers pthread_atfork so the mutex table is not inherited mid-hold.
 *
 * libat_lock_1/unlock_1 are hidden in the toolchain's lock.o; defining
 * all four libat_lock_* symbols here keeps that member out of the link.
 * Hashing matches gcc 16.2.0: WATCH_SIZE 64, NLOCKS 64, cacheline pad.
 *
 * Child handler re-inits rather than unlocks: after fork the child's
 * tid no longer matches the mutex owner word. musl's NORMAL mutex
 * unlock ignores owner, but init is well-defined and portable.
 */
#include <pthread.h>
#include <stdint.h>
#include <stddef.h>

#ifndef PAGE_SIZE
#define PAGE_SIZE 4096
#endif
#ifndef CACHLINE_SIZE
#define CACHLINE_SIZE 64
#endif
#ifndef WATCH_SIZE
#define WATCH_SIZE CACHLINE_SIZE
#endif

struct lock {
	pthread_mutex_t mutex;
	char pad[sizeof(pthread_mutex_t) < CACHLINE_SIZE
			 ? CACHLINE_SIZE - sizeof(pthread_mutex_t)
			 : 0];
};

#define NLOCKS (PAGE_SIZE / WATCH_SIZE)

static struct lock locks[NLOCKS] = {
	[0 ... NLOCKS - 1].mutex = PTHREAD_MUTEX_INITIALIZER
};

static inline uintptr_t addr_hash(void *ptr)
{
	return ((uintptr_t)ptr / WATCH_SIZE) % NLOCKS;
}

static void atfork_prepare(void)
{
	for (size_t i = 0; i < NLOCKS; i++)
		pthread_mutex_lock(&locks[i].mutex);
}

static void atfork_parent(void)
{
	for (size_t i = 0; i < NLOCKS; i++)
		pthread_mutex_unlock(&locks[i].mutex);
}

static void atfork_child(void)
{
	for (size_t i = 0; i < NLOCKS; i++)
		pthread_mutex_init(&locks[i].mutex, NULL);
}

static void install_atfork(void)
{
	pthread_atfork(atfork_prepare, atfork_parent, atfork_child);
}

__attribute__((constructor))
static void init(void)
{
	install_atfork();
}

void libat_lock_1(void *ptr)
{
	pthread_mutex_lock(&locks[addr_hash(ptr)].mutex);
}

void libat_unlock_1(void *ptr)
{
	pthread_mutex_unlock(&locks[addr_hash(ptr)].mutex);
}

void libat_lock_n(void *ptr, size_t n)
{
	uintptr_t h = addr_hash(ptr);
	size_t i = 0;
	size_t nlocks = (n + ((uintptr_t)ptr % WATCH_SIZE) + WATCH_SIZE - 1) / WATCH_SIZE;
	if (nlocks > NLOCKS)
		nlocks = NLOCKS;
	if (__builtin_expect(h + nlocks > NLOCKS, 0)) {
		size_t j = h + nlocks - NLOCKS;
		for (; i < j; ++i)
			pthread_mutex_lock(&locks[i].mutex);
	}
	for (; i < nlocks; ++i)
		pthread_mutex_lock(&locks[h++].mutex);
}

void libat_unlock_n(void *ptr, size_t n)
{
	uintptr_t h = addr_hash(ptr);
	size_t i = 0;
	size_t nlocks = (n + ((uintptr_t)ptr % WATCH_SIZE) + WATCH_SIZE - 1) / WATCH_SIZE;
	if (nlocks > NLOCKS)
		nlocks = NLOCKS;
	if (__builtin_expect(h + nlocks > NLOCKS, 0)) {
		size_t j = h + nlocks - NLOCKS;
		for (; i < j; ++i)
			pthread_mutex_unlock(&locks[i].mutex);
	}
	for (; i < nlocks; ++i)
		pthread_mutex_unlock(&locks[h++].mutex);
}
