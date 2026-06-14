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

/*
 * Follow the symlink chain at `start` to its final target and write that
 * target's basename into `out`. Mirrors resolve_chain_basename() in
 * post-install.sh: bounded depth to defend against cycles, and relative
 * link targets are resolved against the link's own directory.
 *
 * If `start` is not a symlink (or does not exist), `out` ends up as
 * basename(start), so callers can compare against the original name to
 * detect "no alias to follow".
 */
static void resolve_chain_basename(const char *start, char *out,
		size_t outsz) {
	char cur[PATH_MAX];
	strncpy(cur, start, sizeof(cur) - 1);
	cur[sizeof(cur) - 1] = '\0';

	for (int depth = 16; depth > 0; depth--) {
		struct stat st;
		if (lstat(cur, &st) != 0 || !S_ISLNK(st.st_mode))
			break;
		char link[PATH_MAX];
		ssize_t m = readlink(cur, link, sizeof(link) - 1);
		if (m < 0)
			break;
		link[m] = '\0';
		if (link[0] == '/') {
			strncpy(cur, link, sizeof(cur) - 1);
			cur[sizeof(cur) - 1] = '\0';
		} else {
			char dir[PATH_MAX];
			strncpy(dir, cur, sizeof(dir) - 1);
			dir[sizeof(dir) - 1] = '\0';
			char *sl = strrchr(dir, '/');
			if (sl)
				*sl = '\0';
			else
				dir[0] = '\0';
			char joined[PATH_MAX];
			if (snprintf(joined, sizeof(joined), "%s/%s", dir, link)
					>= (int)sizeof(joined))
				break;
			strncpy(cur, joined, sizeof(cur) - 1);
			cur[sizeof(cur) - 1] = '\0';
		}
	}

	const char *base = strrchr(cur, '/');
	base = base ? base + 1 : cur;
	strncpy(out, base, outsz - 1);
	out[outsz - 1] = '\0';
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
		/*
		 * The invoked name may be a consumer-created alias added after
		 * install (e.g. `strip -> x86_64-linux-musl-strip`), for which
		 * post-install.sh created no matching `.real/` entry. If a
		 * sibling alias of the same name exists, follow its symlink
		 * chain to the final basename and retry `.real/<final>`.
		 */
		char alias_path[PATH_MAX];
		if (snprintf(alias_path, sizeof(alias_path), "%s/%s",
				self_dir, invoked_base)
				< (int)sizeof(alias_path)) {
			char final_base[PATH_MAX];
			resolve_chain_basename(alias_path, final_base,
				sizeof(final_base));
			if (final_base[0]
				&& strcmp(final_base, invoked_base) != 0) {
				char alt[PATH_MAX];
				if (snprintf(alt, sizeof(alt), "%s/.real/%s",
						self_dir, final_base)
						< (int)sizeof(alt)
					&& file_exists(alt)) {
					strncpy(real_binary, alt,
						sizeof(real_binary) - 1);
					real_binary[sizeof(real_binary) - 1] =
						'\0';
				}
			}
		}
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
