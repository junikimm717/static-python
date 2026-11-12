# musl's src/signal/s390x/sigsetjmp.s with jz -> jgz: the 16-bit branch to
# ___setjmp in another object wraps silently past 64 KiB. Delete once the
# gccfactory toolchain carries the fix. See staticpy-traps S390X_SIGSETJMP_PC16.
	.global sigsetjmp
	.global __sigsetjmp
	.type sigsetjmp,%function
	.type __sigsetjmp,%function
	.hidden ___setjmp
	.hidden __sigsetjmp_tail
sigsetjmp:
__sigsetjmp:
	ltgr  %r3, %r3
	jgz   ___setjmp

	stg   %r14, 18*8(%r2)
	stg   %r6,  20*8(%r2)
	lgr   %r6,  %r2

	brasl %r14, ___setjmp

	lgr   %r3,  %r2
	lgr   %r2,  %r6
	lg    %r14, 18*8(%r2)
	lg    %r6,  20*8(%r2)

	jg __sigsetjmp_tail

	.section .note.GNU-stack,"",@progbits
