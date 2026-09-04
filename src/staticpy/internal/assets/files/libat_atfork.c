/* Drop-in replacement for gcc libatomic config/posix/lock.c.
 *
 * gcc's table is 64 pthread_mutex_t with no pthread_atfork: a child
 * forked while another thread holds a slot hangs on its first 64-bit
 * atomic. We keep the same hash (WATCH_SIZE 64, NLOCKS 64) so our
 * four libat_lock_* symbols replace lock.o, but the slots are
 * lock-free 32-bit spinlocks (atomic_uint is lock-free on every
 * libatomic triple) and the child handler just zeros the table.
 *
 * prepare/parent are empty on purpose. Locking all 64 mutexes there
 * deadlocks with CPython's stop-the-world: a thread paused mid-atomic
 * still owns its slot. Re-init does not need the owner, and a store
 * of zero is async-signal-safe (mutex_init is not).
 *
 * Slots store the holder's getpid(), not a 0/1 flag. pthread_atfork
 * child handlers run in reverse registration order, so CPython's
 * AfterFork (registered later, or called after fork() returns) can
 * do a 64-bit atomic before our zero runs. A child pid does not
 * match the inherited word and steals. Same-process waiters see
 * their own pid and spin.
 *
 * See staticpy-traps: "A hung forked child is libatomic, not qemu."
 */
#include <pthread.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stddef.h>
#include <sched.h>
#include <unistd.h>

#ifndef PAGE_SIZE
#define PAGE_SIZE 4096
#endif
#ifndef CACHLINE_SIZE
#define CACHLINE_SIZE 64
#endif
#ifndef WATCH_SIZE
#define WATCH_SIZE CACHLINE_SIZE
#endif

#define NLOCKS (PAGE_SIZE / WATCH_SIZE)

static atomic_uint locks[NLOCKS];

static inline uintptr_t addr_hash(void *ptr)
{
	return ((uintptr_t)ptr / WATCH_SIZE) % NLOCKS;
}

static unsigned self_token(void)
{
	unsigned u = (unsigned)getpid();
	return u ? u : 1u;
}

static void lock_one(size_t i)
{
	unsigned me = self_token();
	for (;;) {
		unsigned expected = 0;
		if (atomic_compare_exchange_strong_explicit(&locks[i], &expected, me,
							    memory_order_acquire,
							    memory_order_relaxed))
			return;
		if (expected != 0 && expected != me &&
		    atomic_compare_exchange_strong_explicit(&locks[i], &expected, me,
							    memory_order_acquire,
							    memory_order_relaxed))
			return;
		sched_yield();
	}
}

static void unlock_one(size_t i)
{
	atomic_store_explicit(&locks[i], 0u, memory_order_release);
}

static void atfork_child(void)
{
	for (size_t i = 0; i < NLOCKS; i++)
		atomic_store_explicit(&locks[i], 0u, memory_order_relaxed);
}

__attribute__((constructor))
static void init(void)
{
	pthread_atfork(NULL, NULL, atfork_child);
}

void libat_lock_1(void *ptr)
{
	lock_one(addr_hash(ptr));
}

void libat_unlock_1(void *ptr)
{
	unlock_one(addr_hash(ptr));
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
			lock_one(i);
	}
	for (; i < nlocks; ++i)
		lock_one(h++);
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
			unlock_one(i);
	}
	for (; i < nlocks; ++i)
		unlock_one(h++);
}
