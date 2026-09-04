# libatomic lock table + multithreaded fork

## Summary

`ThreadJoinOnShutdown.test_reinit_tls_after_fork` hangs as *env changed*
("process still running after 300s", then `qemu: uncaught target signal 6`)
on every triple whose 64-bit atomics go through gcc `libatomic`'s mutex
table: `arm-linux-musleabi` and `riscv32-linux-musl`. The same method
passes on `arm-linux-musleabihf` (8-byte atomics are lock-free) and on
`i386-linux-musl` (libatomic still has a locked fallback, but qemu-i386
plus the old mutex atfork usually wins the race).

This is not a dropped constructor, not CFLAGS leaking `-flto`, and not a
qemu-arm 11.1.1 regression. Host qemu-arm 8.2.2 hangs the same way.
Raising arm-eabi to armv7 would hide it the way armhf does, and would
not help rv32.

## Minimal reproducer

Stock gcc `lock.c` (no atfork), 16 forking threads, 4 workers doing
`atomic_fetch_add` on a `uint64_t`:

```
# dist/work/libat-repro/atomic_fork16.c linked with only -latomic
timeout 10 qemu-arm -L $SYS ./af16-stock-arm-linux-musleabi 16 4
# timeout child pid=...  ok=11 fail=5
```

Same binary with `libat_atfork.o` before `-latomic` finishes 16/16.
CPython's test is the same pattern plus `PyOS_BeforeFork`'s
stop-the-world; that is why a C probe can be solid while
`python -m test test_threading -m test_reinit_tls_after_fork` is flaky
or hangs.

Lock-free sizes under the same qemus (`atomic_is_lock_free`):

| triple              | 4-byte | 8-byte | test_reinit_tls |
|---------------------|--------|--------|-----------------|
| arm-linux-musleabi  | 1      | 0      | hangs           |
| riscv32-linux-musl  | 1      | 0      | hangs           |
| i386-linux-musl     | 1      | 0      | passes          |
| arm-linux-musleabihf| 1      | 1      | passes          |

armhf's `__atomic_fetch_add_8` does not reference `libat_lock_1`.
i386/eabi/rv32's do.

## Root cause

gcc `libatomic` `config/posix/lock.c` is a 64-entry `pthread_mutex_t`
table, hashed by address, with no `pthread_atfork`. A child forked
while another thread holds a slot inherits the owner word and blocks
forever on its first non-lock-free atomic. CPython 3.13 uses 64-bit
atomics in the interpreter; `os.fork` from a worker then
`PyOS_AfterFork_Child` is enough to hit one.

The first replacement (`libat_atfork.c` as a mutex table plus
`pthread_atfork`) was the right idea and the right link recipe:

- compile argv is `-static -pthread -O2 -c` (no `-flto`); gcc does
  **not** read `CFLAGS` from the environment on a raw `cc` invocation
- `LIBS=` puts `libat_atfork.o` before `-latomic`
- the stripped arm-eabi binary still has `.init_array` →
  `pthread_atfork(prepare, parent, child)` and the four `libat_lock_*`
  bodies (compare `objdump -d` of the `.o`)

What was wrong was the handlers. `prepare` locked all 64 mutexes so
the child could unlock or re-init as the owner. That deadlocks with
CPython's stop-the-world: a thread paused mid-atomic still owns its
slot, `prepare` waits for it, and a child that is created anyway can
inherit an all-locked table if the child handler then misbehaves.
`pthread_mutex_init` in the child is also not async-signal-safe.

i386 passing with that code is the race plus a faster qemu, not a
proof the mutex prepare was correct. A C hammer of 16 forks + 64-bit
atomics hangs on i386 too when linked against stock `-latomic` only.

## What we ended up doing

`libat_atfork.c` still replaces the four `libat_lock_*` symbols and
still registers `pthread_atfork`, but the slots are `atomic_uint`
spinlocks (4-byte CAS is lock-free on every libatomic triple, so this
does not recurse into the table) and the child handler only stores
zero. `prepare` / `parent` are empty: re-init does not need the owner.
The store is async-signal-safe.

The compile/link recipe is unchanged; the `.c` hash is already in the
pycross key. Do not bump `recipe.Version`. Do not add `[expect]`.
Do not raise the ARM ISA.

## What we'd want upstream

gcc `libatomic` should register `pthread_atfork` and re-init the
table in the child, ideally without taking all 64 mutexes in
`prepare`. Until then the object we link in front of `-latomic` is
the portable fix.
