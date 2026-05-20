#!/usr/bin/env perl
#
# parallel-pythons.pl - supervise N concurrent static Python builds.
#
# Runs inside the dev container, inside a tmux session (the user owns that
# part). Builds the native interpreter serially first (cross builds need a
# runnable python-static-$(NATIVE_TARGET)/bin/python*), then fans out cross
# targets with up to --jobs workers, each exec'ing `make ... python3` with
# stdout+stderr redirected to build-logs/python-static-<platform>.log.
#
# Race story: external tarballs under tarballs/ are fetched by curl rules in
# the top-level Makefile. Two parallel `make python3` runs can race the same
# download. We sidestep that by running `make download` once as a preflight.
#
# Perl-core only: strict, warnings, POSIX, Time::HiRes, Getopt::Long,
# File::Path, Cwd, FindBin. No CPAN.

use strict;
use warnings;

use POSIX qw(:sys_wait_h strftime);
use Time::HiRes qw(time sleep);
use Getopt::Long qw(:config gnu_getopt no_ignore_case);
use File::Path qw(make_path);
use FindBin qw($RealBin);

# ---- defaults ----------------------------------------------------------

my %opt = (
	jobs          => 4,
	make_jobs     => 8,
	log_dir       => 'build-logs',
	download      => 1,
	fail_fast     => 1,
	force         => 0,
	use_crossmake => 0,
	status_int    => 60,
);

sub usage {
	my $rc = shift // 0;
	my $fh = $rc ? \*STDERR : \*STDOUT;
	print {$fh} <<'EOF';
usage: parallel-pythons.pl [options] [platform ...]

  -j, --jobs N           parallel cross workers          (default 4)
  -J, --make-jobs N      JOBS env passed per cross build (default 8)
      --log-dir DIR      per-platform logs               (default build-logs)
      --no-download      skip the `make download` preflight
      --use-crossmake    pass USE_CROSSMAKE=1 to inner builds (default 0)
  -k, --keep-going       do not abort on first failure   (default: fail-fast)
      --force            rebuild even if interpreter already present
      --status-interval N  seconds between running summaries (default 60)
  -h, --help             this message

Platforms default to the non-comment lines of supported.txt.

Per-platform logs land in <log-dir>/python-static-<platform>.log. The native
preflight uses the same path for the host triple. Status lines go to stderr;
the final summary table goes to stdout.
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
	'use-crossmake',
	'status-interval=i' => \$opt{status_int},
	'help|h' => sub { usage(0) },
) or usage(2);

$opt{jobs}      >= 1 or die "--jobs must be >= 1\n";
$opt{make_jobs} >= 1 or die "--make-jobs must be >= 1\n";
$opt{status_int} >= 1 or die "--status-interval must be >= 1\n";

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

# ---- arch / python version helpers ------------------------------------

chomp(my $native_arch = `uname -m`);
$native_arch ne '' or die "uname -m returned nothing\n";

chomp(my $pythonv = `make print-PYTHONV`);
$pythonv ne '' or die "make print-PYTHONV returned nothing\n";

my $native_platform = "$native_arch-linux-musl";

sub split_platform {
	my $platform = shift;
	my ($arch, $kernel, $abi) = split /-/, $platform, 3;
	$kernel eq 'linux' or die "bad platform '$platform' (kernel != linux)\n";
	$abi ne '' or die "bad platform '$platform' (missing ABI)\n";
	return ($arch, $kernel, $abi);
}

sub interpreter_for {
	my $platform = shift;
	return "python-static-$platform/bin/python$pythonv";
}

sub logfile_for {
	my $platform = shift;
	return "$opt{log_dir}/python-static-$platform.log";
}

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

sub append_log_footer {
	my ($logfile, $exit, $sig, $elapsed) = @_;
	return unless open my $fh, '>>', $logfile;
	print {$fh} "=== finished: ",
		strftime('%FT%TZ', gmtime),
		"  exit=$exit  sig=$sig  elapsed=", hms($elapsed), "\n";
	close $fh;
}

sub run_logged_make {
	my ($platform, $jobs, @make_args) = @_;
	my $logfile = logfile_for($platform);
	my $started = time();

	open my $logfh, '>>', $logfile
		or die "cannot open $logfile: $!\n";
	print {$logfh} "=== platform: $platform\n";
	print {$logfh} "=== started:  ", strftime('%FT%TZ', gmtime), "\n";
	print {$logfh} "=== JOBS=$jobs USE_CROSSMAKE=$opt{use_crossmake}\n";
	$logfh->autoflush(1);

	local *STDOUT = $logfh;
	local *STDERR = $logfh;
	$ENV{JOBS} = $jobs;
	my $rc = system(@make_args);
	my $exit = $rc == -1 ? -1 : ($rc >> 8);
	my $sig  = $rc == -1 ? 0  : ($rc & 0x7f);
	my $elapsed = time() - $started;
	append_log_footer($logfile, $exit, $sig, $elapsed);
	return ($exit, $sig, $elapsed, $logfile);
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

# ---- preflight: native interpreter (serial) ---------------------------

my @results;  # [ platform, status, elapsed, exit, sig, logfile ]

sub build_native_preflight {
	my $interp = interpreter_for($native_platform);
	if (!$opt{force} && -f $interp) {
		stderr_log("skip   $native_platform  native already built ($interp)");
		return 1;
	}

	stderr_log("preflight: native python3 $native_platform");
	my $jobs = int(`nproc`);
	$jobs >= 1 or die "nproc returned nothing usable\n";

	my @make = (
		'make',
		"USE_CROSSMAKE=$opt{use_crossmake}",
		"JOBS=$jobs",
		'python3',
	);
	my ($exit, $sig, $elapsed, $logfile) = run_logged_make(
		$native_platform, $jobs, @make);

	if ($exit == 0 && $sig == 0) {
		stderr_log(sprintf("OK     %-24s (%s) [native preflight]",
			$native_platform, hms($elapsed)));
		push @results, [$native_platform, 'OK', $elapsed, 0, 0, $logfile];
		return 1;
	}

	my $how = $sig ? "killed by signal $sig" : "exit=$exit";
	stderr_log(sprintf("FAIL   %-24s (%s, %s) [native preflight] -> %s",
		$native_platform, hms($elapsed), $how, $logfile));
	push @results, [$native_platform, 'FAIL', $elapsed, $exit, $sig, $logfile];
	return 0;
}

build_native_preflight()
	or exit 1;

# Cross workers only; native was handled above.
my @cross_requested = grep { $_ ne $native_platform } @requested;

# ---- skip already-built unless --force --------------------------------

unless ($opt{force}) {
	my @keep;
	for my $p (@cross_requested) {
		my $interp = interpreter_for($p);
		if (-f $interp) {
			stderr_log("skip   $p  already built ($interp)");
		} else {
			push @keep, $p;
		}
	}
	@cross_requested = @keep;
}

make_path($opt{log_dir}) unless -d $opt{log_dir};
-d $opt{log_dir} or die "log dir '$opt{log_dir}' is missing and could not be created\n";

if (!@cross_requested) {
	stderr_log("nothing more to build");
	print_summary();
	my $any_bad = scalar grep { $_->[1] ne 'OK' } @results;
	exit($any_bad ? 1 : 0);
}

# ---- worker pool -------------------------------------------------------

my @queue   = @cross_requested;
my %running;  # pid => { platform, started, logfile }
my $aborting = 0;

sub spawn_worker {
	my $platform = shift @queue;
	return unless defined $platform;

	my ($arch, undef, $abi) = split_platform($platform);
	my $logfile = logfile_for($platform);
	my $pid = fork();
	defined $pid or die "fork: $!\n";

	if ($pid == 0) {
		open STDIN,  '<', '/dev/null'    or die "child stdin: $!\n";
		open STDOUT, '>>', $logfile      or die "child stdout: $!\n";
		open STDERR, '>&', \*STDOUT      or die "child stderr: $!\n";
		print "=== platform: $platform\n";
		print "=== started:  ", strftime('%FT%TZ', gmtime), "\n";
		print "=== JOBS=$opt{make_jobs} USE_CROSSMAKE=$opt{use_crossmake}\n";
		STDOUT->autoflush(1);

		$ENV{JOBS} = $opt{make_jobs};
		exec(
			'make',
			"USE_CROSSMAKE=$opt{use_crossmake}",
			"JOBS=$opt{make_jobs}",
			"ARCH=$arch",
			"MUSLABI=$abi",
			'python3',
		) or die "exec make python3: $!\n";
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
	return 0 unless $rec;

	my $exit    = $status >> 8;
	my $sig     = $status & 0x7f;
	my $elapsed = time() - $rec->{started};

	append_log_footer($rec->{logfile}, $exit, $sig, $elapsed);

	if ($exit == 0 && $sig == 0) {
		stderr_log(sprintf("OK     %-24s (%s)",
			$rec->{platform}, hms($elapsed)));
		push @results, [$rec->{platform}, 'OK', $elapsed, 0, 0, $rec->{logfile}];
	} else {
		my $how = $sig ? "killed by signal $sig" : "exit=$exit";
		my $tag = $sig ? 'KILLED' : 'FAIL';
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
	"jobs=%d make-jobs=%d log-dir=%s fail-fast=%s use-crossmake=%s cross=%d",
	$opt{jobs}, $opt{make_jobs}, $opt{log_dir},
	$opt{fail_fast} ? 'yes' : 'no',
	$opt{use_crossmake} ? 'yes' : 'no',
	scalar @cross_requested));

my $last_status = time();

while (@queue || %running) {
	handle_signal();

	while (!$aborting && @queue && (scalar keys %running) < $opt{jobs}) {
		spawn_worker();
	}

	while (reap_one(0)) { }
	sleep 0.5;

	if (time() - $last_status >= $opt{status_int}) {
		print_status();
		$last_status = time();
	}
}

handle_signal();
print_summary();

my $any_bad = scalar grep { $_->[1] ne 'OK' } @results;
exit($any_bad ? 1 : 0);

# ---- summary -----------------------------------------------------------

sub print_summary {
	print "=== summary ===\n";
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
	if (@queue) {
		print "  -- not started (fail-fast or signal):\n";
		print "     $_\n" for @queue;
	}
}
