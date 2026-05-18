/*
 * Relocatable launcher for dynamic host binaries in the toolchain tarball.
 *
 * See ai/PORTABILITY_PROOF.md for the end-to-end test that proves this
 * trick lets a static-musl toolchain run on a glibc-only rootfs.
 *
 * Each real binary (gcc, cc1, ld.bfd, ...) is moved to a sibling `.real/`
 * directory and replaced with a copy of this launcher. At run time we:
 *
 *   - resolve our location via /proc/self/exe;
 *   - walk upward until we find `runtime/libc.so` (the bundled musl loader,
 *     which doubles as libc on musl);
 *   - exec the loader with `--library-path runtime/ --argv0 <argv[0]>
 *     <our-dir>/.real/<basename(argv[0])> <argv[1..]>`.
 *
 * Using basename(argv[0]) rather than basename(self) lets one launcher
 * binary answer to multiple names via symlinks (e.g. cc -> gcc): callers
 * see argv[0] preserved, and gcc's driver uses it to decide cc1 vs cc1plus.
 *
 * Built statically with the host compiler so the launcher itself has zero
 * runtime deps.
 */

#define _GNU_SOURCE
#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#ifndef PATH_MAX
#define PATH_MAX 4096
#endif

static int file_exists(const char *p) {
	struct stat st;
	return stat(p, &st) == 0;
}

int main(int argc, char **argv, char **envp) {
	char self[PATH_MAX];
	ssize_t n = readlink("/proc/self/exe", self, sizeof(self) - 1);
	if (n < 0) {
		perror("tc-wrapper: readlink /proc/self/exe");
		return 127;
	}
	self[n] = '\0';

	char self_dir[PATH_MAX];
	strncpy(self_dir, self, sizeof(self_dir) - 1);
	self_dir[sizeof(self_dir) - 1] = '\0';
	char *last_slash = strrchr(self_dir, '/');
	if (!last_slash) {
		fprintf(stderr,
			"tc-wrapper: /proc/self/exe has no slash: %s\n", self);
		return 127;
	}
	*last_slash = '\0';

	const char *invoked =
		(argc > 0 && argv[0] && argv[0][0]) ? argv[0] : self;
	const char *invoked_base = strrchr(invoked, '/');
	invoked_base = invoked_base ? invoked_base + 1 : invoked;
	if (!*invoked_base) {
		fprintf(stderr, "tc-wrapper: empty argv[0] basename\n");
		return 127;
	}

	char real_binary[PATH_MAX];
	if (snprintf(real_binary, sizeof(real_binary),
			"%s/.real/%s", self_dir, invoked_base)
			>= (int)sizeof(real_binary)) {
		fprintf(stderr, "tc-wrapper: real-binary path too long\n");
		return 127;
	}
	if (!file_exists(real_binary)) {
		fprintf(stderr, "tc-wrapper: missing real binary: %s\n",
			real_binary);
		return 127;
	}

	char root[PATH_MAX];
	strncpy(root, self_dir, sizeof(root) - 1);
	root[sizeof(root) - 1] = '\0';

	char loader[PATH_MAX];
	char libdir[PATH_MAX];
	int found = 0;
	for (;;) {
		if (snprintf(loader, sizeof(loader),
				"%s/runtime/libc.so", root)
				< (int)sizeof(loader)
			&& file_exists(loader)) {
			snprintf(libdir, sizeof(libdir), "%s/runtime", root);
			found = 1;
			break;
		}
		char *up = strrchr(root, '/');
		if (!up) break;
		if (up == root) {
			if (root[1] == '\0') break;
			root[1] = '\0';
			if (snprintf(loader, sizeof(loader),
					"%s/runtime/libc.so", root)
					< (int)sizeof(loader)
				&& file_exists(loader)) {
				snprintf(libdir, sizeof(libdir),
					"%s/runtime", root);
				found = 1;
			}
			break;
		}
		*up = '\0';
	}
	if (!found) {
		fprintf(stderr,
			"tc-wrapper: could not locate runtime/libc.so walking up from %s\n",
			self_dir);
		return 127;
	}

	char **new_argv = calloc((size_t)argc + 6, sizeof(char *));
	if (!new_argv) {
		perror("tc-wrapper: calloc");
		return 127;
	}
	int i = 0;
	new_argv[i++] = loader;
	new_argv[i++] = (char *)"--library-path";
	new_argv[i++] = libdir;
	new_argv[i++] = (char *)"--argv0";
	new_argv[i++] = (char *)invoked;
	new_argv[i++] = real_binary;
	for (int j = 1; j < argc; j++) {
		new_argv[i++] = argv[j];
	}
	new_argv[i] = NULL;

	execve(loader, new_argv, envp);
	fprintf(stderr, "tc-wrapper: execve(%s) failed: %s\n",
		loader, strerror(errno));
	return 127;
}
