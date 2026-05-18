#!/usr/bin/env perl
#
# parallel-toolchains.pl - supervise N concurrent toolchain builds.
#
# Runs inside the dev container, inside a tmux session (the user owns that
# part). Pulls platforms off a queue and spawns up to --jobs workers, each
# exec'ing `./toolchains.pl <platform>` with stdout+stderr redirected to
# build-logs/toolchain-<platform>.log.
#
# Race story: musl-cross-make's `sources/%` rule wgets into a directory
# that every per-target tree symlinks back to `tarballs/`. Two parallel
# `make crossmake` runs hitting the same gcc tarball corrupt one tmp file.
# We sidestep the race entirely by running `make download` once as a
# preflight, which fetches every tarball serially into `tarballs/`.
# Workers then only ever read from that cache; the wget rule is never
# triggered concurrently.
#
# Perl-core only: strict, warnings, POSIX, Time::HiRes, Getopt::Long,
# File::Path, File::Basename, Cwd. No CPAN.

use strict;
use warnings;

use POSIX qw(:sys_wait_h strftime);
use Time::HiRes qw(time sleep);
use Getopt::Long qw(:config gnu_getopt no_ignore_case);
use File::Path qw(make_path);
use File::Basename qw(basename dirname);
use Cwd qw(abs_path);
use FindBin qw($RealBin);

# ---- defaults ----------------------------------------------------------

my %opt = (
	jobs       => 4,
	make_jobs  => 8,
	log_dir    => 'build-logs',
	download   => 1,
	fail_fast  => 1,
	force      => 0,
	status_int => 60,
);

sub usage {
	my $rc = shift // 0;
	my $fh = $rc ? \*STDERR : \*STDOUT;
	print {$fh} <<'EOF';
usage: parallel-toolchains.pl [options] [platform ...]

  -j, --jobs N           parallel workers              (default 4)
  -J, --make-jobs N      JOBS env passed per build     (default 8)
      --log-dir DIR      per-platform logs             (default build-logs)
      --no-download      skip the `make download` preflight
  -k, --keep-going       do not abort on first failure (default: fail-fast)
      --force            rebuild even if tarball already present
      --status-interval N  seconds between running summaries (default 60)
  -h, --help             this message

Platforms default to the non-comment lines of supported.txt.

Per-platform logs land in <log-dir>/toolchain-<platform>.log. Status lines
go to stderr; the final summary table goes to stdout.
EOF
	exit $rc;
}

GetOptions(\%opt,
	'jobs|j=i',
	'make-jobs|J=i',
	'log-dir=s',
	'download!',
	'keep-going|k' => sub { $opt{fail_fast} = 0 },
	'fail-fast'    => sub { $opt{fail_fast} = 1 },
	'force',
	'status-interval=i' => \$opt{status_int},
	'help|h' => sub { usage(0) },
) or usage(2);

$opt{jobs}      >= 1 or die "--jobs must be >= 1\n";
$opt{make_jobs} >= 1 or die "--make-jobs must be >= 1\n";
$opt{status_int} >= 1 or die "--status-interval must be >= 1\n";

# Resolve from script dir, not cwd. The supervisor must always launch
# toolchains.pl and make from the repo root regardless of who called us.
chdir $RealBin or die "chdir $RealBin: $!\n";

# ---- platform validation ----------------------------------------------

sub read_supported {
	open my $fh, '<', 'supported.txt'
		or die "cannot open supported.txt: $!\n";
	my @p;
	while (<$fh>) {
		chomp;
		s/#.*//;
		s/^\s+|\s+$//g;
		next if $_ eq '';
		push @p, $_;
	}
	close $fh;
	return @p;
}

my @supported = read_supported();
my %is_supported = map { $_ => 1 } @supported;

my @requested = @ARGV ? @ARGV : @supported;
for my $p (@requested) {
	$is_supported{$p}
		or die "platform '$p' is not in supported.txt\n";
}

# ---- arch / tctype helpers --------------------------------------------

chomp(my $native_arch = `uname -m`);
$native_arch ne '' or die "uname -m returned nothing\n";

sub split_platform {
	my $platform = shift;
	my ($arch, $kernel, $abi) = split /-/, $platform, 3;
	$kernel eq 'linux' or die "bad platform '$platform' (kernel != linux)\n";
	return ($arch, $kernel, $abi);
}

sub tctype_for {
	my $platform = shift;
	my ($arch) = split_platform($platform);
	return $arch eq $native_arch ? 'native' : 'cross';
}

sub tarball_for {
	my $platform = shift;
	my $tctype = tctype_for($platform);
	return "tarballs/$platform-$tctype.tgz";
}

# ---- preflight: make download -----------------------------------------

if ($opt{download}) {
	stderr_log("preflight: make download");
	my $rc = system('make', 'download');
	if ($rc != 0) {
		my $exit = $rc == -1 ? -1 : ($rc >> 8);
		die "make download failed (exit=$exit); aborting before fan-out\n";
	}
}

# ---- skip already-built unless --force --------------------------------

unless ($opt{force}) {
	my @keep;
	for my $p (@requested) {
		my $tb = tarball_for($p);
		if (-f $tb) {
			stderr_log("skip   $p  already built ($tb)");
		} else {
			push @keep, $p;
		}
	}
	@requested = @keep;
}

if (!@requested) {
	stderr_log("nothing to build");
	exit 0;
}

# ---- log dir -----------------------------------------------------------

make_path($opt{log_dir}) unless -d $opt{log_dir};
-d $opt{log_dir} or die "log dir '$opt{log_dir}' is missing and could not be created\n";

# ---- worker pool -------------------------------------------------------

my @queue   = @requested;
my %running;  # pid => { platform, started, logfile }
my @results;  # ordered: [ platform, status('OK'|'FAIL'|'KILLED'), elapsed, exit, sig, logfile ]
my $aborting = 0;

sub iso_now { strftime('%H:%M:%S', localtime) }

sub stderr_log {
	my $msg = shift;
	print STDERR sprintf("[%s] %s\n", iso_now(), $msg);
}

sub hms {
	my $s = int(shift);
	my $h = int($s / 3600); $s -= $h * 3600;
	my $m = int($s /   60); $s -= $m *   60;
	return $h ? sprintf('%dh%02dm%02ds', $h, $m, $s)
	         : $m ? sprintf('%dm%02ds', $m, $s)
	              : sprintf('%ds', $s);
}

sub spawn_worker {
	my $platform = shift @queue;
	return unless defined $platform;

	my $logfile = "$opt{log_dir}/toolchain-$platform.log";
	my $pid = fork();
	defined $pid or die "fork: $!\n";

	if ($pid == 0) {
		# child
		open STDIN,  '<', '/dev/null'    or die "child stdin: $!\n";
		open STDOUT, '>>', $logfile      or die "child stdout: $!\n";
		open STDERR, '>&', \*STDOUT      or die "child stderr: $!\n";
		# Header so each log self-identifies for tail -F users.
		print "=== platform: $platform\n";
		print "=== started:  ", strftime('%FT%TZ', gmtime), "\n";
		print "=== JOBS=$opt{make_jobs}\n";
		STDOUT->autoflush(1);

		$ENV{JOBS} = $opt{make_jobs};
		exec("./toolchains.pl", $platform)
			or die "exec ./toolchains.pl: $!\n";
	}

	$running{$pid} = {
		platform => $platform,
		started  => time(),
		logfile  => $logfile,
	};
	stderr_log(sprintf("start  %-24s -> %s", $platform, $logfile));
}

sub reap_one {
	my $blocking = shift;
	my $flags = $blocking ? 0 : WNOHANG;
	my $pid = waitpid(-1, $flags);
	return 0 if $pid <= 0;

	my $status = $?;
	my $rec = delete $running{$pid};
	return 0 unless $rec;  # not one of ours, shouldn't happen

	my $exit    = $status >> 8;
	my $sig     = $status & 0x7f;
	my $elapsed = time() - $rec->{started};

	# Append a footer to the per-platform log too, so a tail -F reader
	# sees the same outcome the supervisor reports.
	if (open my $fh, '>>', $rec->{logfile}) {
		print {$fh} "=== finished: ",
			strftime('%FT%TZ', gmtime),
			"  exit=$exit  sig=$sig  elapsed=", hms($elapsed), "\n";
		close $fh;
	}

	if ($exit == 0 && $sig == 0) {
		stderr_log(sprintf("OK     %-24s (%s)",
			$rec->{platform}, hms($elapsed)));
		push @results, [$rec->{platform}, 'OK', $elapsed, 0, 0, $rec->{logfile}];
	} else {
		my $how = $sig ? "killed by signal $sig" : "exit=$exit";
		my $tag = $sig ? 'KILLED' : 'FAIL';
		# A worker we SIGTERM'd during fail-fast tear-down is reported
		# as KILLED, not FAIL, to make the summary readable.
		if ($aborting && $sig) {
			$tag = 'KILLED';
		}
		stderr_log(sprintf("%-6s %-24s (%s, %s) -> %s",
			$tag, $rec->{platform}, hms($elapsed), $how, $rec->{logfile}));
		push @results,
			[$rec->{platform}, $tag, $elapsed, $exit, $sig, $rec->{logfile}];

		if ($opt{fail_fast} && !$aborting && $tag eq 'FAIL') {
			$aborting = 1;
			stderr_log("fail-fast: terminating "
				. (scalar keys %running) . " running worker(s)");
			drain_kill();
		}
	}
	return 1;
}

sub drain_kill {
	for my $pid (keys %running) {
		kill 'TERM', $pid;
	}
	# Give workers a chance to exit cleanly under SIGTERM, then SIGKILL
	# anything still alive after 10s. We don't want a runaway gcc to
	# hold the supervisor hostage.
	my $deadline = time() + 10;
	while (%running && time() < $deadline) {
		reap_one(0);
		sleep 0.2;
	}
	for my $pid (keys %running) {
		kill 'KILL', $pid;
	}
	while (%running) {
		reap_one(1);
	}
}

sub print_status {
	return unless %running;
	my @parts;
	for my $pid (sort { $running{$a}{started} <=> $running{$b}{started} } keys %running) {
		my $r = $running{$pid};
		push @parts, sprintf("%s(%s)", $r->{platform}, hms(time() - $r->{started}));
	}
	my $done = scalar grep { $_->[1] eq 'OK' } @results;
	my $fail = scalar grep { $_->[1] ne 'OK' } @results;
	stderr_log(sprintf("running %d/%d: %s  | queued %d  done %d  failed %d",
		scalar keys %running, $opt{jobs},
		join('  ', @parts),
		scalar @queue, $done, $fail));
}

# ---- signal handlers ---------------------------------------------------

my $caught_signal = 0;
$SIG{INT}  = sub { $caught_signal = 'INT' };
$SIG{TERM} = sub { $caught_signal = 'TERM' };
# Default $SIG{CHLD} (we waitpid explicitly).
# Ignore SIGPIPE so a closed log pipe doesn't kill us.
$SIG{PIPE} = 'IGNORE';

sub handle_signal {
	return unless $caught_signal;
	my $sig = $caught_signal;
	$caught_signal = 0;
	stderr_log("caught SIG$sig, tearing down");
	$aborting = 1;
	drain_kill();
	print_summary();
	exit 130;
}

# ---- main loop ---------------------------------------------------------

stderr_log(sprintf(
	"jobs=%d make-jobs=%d log-dir=%s fail-fast=%s platforms=%d",
	$opt{jobs}, $opt{make_jobs}, $opt{log_dir},
	$opt{fail_fast} ? 'yes' : 'no', scalar @requested));

my $last_status = time();

while (@queue || %running) {
	handle_signal();

	while (!$aborting && @queue && (scalar keys %running) < $opt{jobs}) {
		spawn_worker();
	}

	# Reap anything that finished while we weren't looking, then sleep
	# briefly. A 0.5s tick is plenty for a workload measured in minutes,
	# and keeps the periodic status update responsive.
	while (reap_one(0)) { }
	sleep 0.5;

	if (time() - $last_status >= $opt{status_int}) {
		print_status();
		$last_status = time();
	}
}

handle_signal();
print_summary();

# Exit non-zero if any platform failed (or was killed mid-run).
my $any_bad = scalar grep { $_->[1] ne 'OK' } @results;
exit($any_bad ? 1 : 0);

# ---- summary -----------------------------------------------------------

sub print_summary {
	print "=== summary ===\n";
	# Width-pad the platform column for readability.
	my $w = 0;
	$w = length($_->[0]) > $w ? length($_->[0]) : $w for @results;
	$w ||= 24;
	for my $r (@results) {
		my ($plat, $status, $elapsed, $exit, $sig, $logfile) = @$r;
		if ($status eq 'OK') {
			printf("  %-${w}s  OK     %s\n", $plat, hms($elapsed));
		} else {
			my $detail = $sig ? "sig=$sig" : "exit=$exit";
			printf("  %-${w}s  %-6s %s  %s  %s\n",
				$plat, $status, hms($elapsed), $detail, $logfile);
		}
	}
	# Anything that never started (queue not drained, e.g. fail-fast)
	# is worth listing too.
	if (@queue) {
		print "  -- not started (fail-fast or signal):\n";
		print "     $_\n" for @queue;
	}
}
