#!/usr/bin/env perl

use strict;
use warnings;

use FindBin qw($RealDir);

my $nativearch = `uname -m`;
chomp $nativearch;

my $cores = int(`nproc`);
my $jobs = $cores > 8 ? 8 : $cores;

chdir $RealDir || die "failed cding??";

my @supported = ();
open my $sh, '<', "./supported.txt" || die "could not open file!";
while (<$sh>) {
  chomp;
  if (length != 0) {
    push @supported, $_;
  }
}
close $sh;

sub is_supported {
  my ($platform) = @_;
  foreach my $pltm (@supported) {
    if ($pltm eq $platform) {
      return 1;
    }
  }
  return 0;
}

sub build {
  chdir $RealDir || die "failed cding??";
  my ($platform) = @_;
  my ($arch, $kernel, $abi) = split /-/, $platform;
  $kernel eq "linux" || die "Did not get a linux platform";
  $abi =~ /^musl(eabihf)?$/ || die "did not get an appropriate musl abi $abi";
  $platform = "$arch-$kernel-$abi";
  print "compiling toolchain for platform $platform...\n";

  my $tctype = "cross";
  if ($nativearch eq $arch) {
    $tctype = "native";
  }
  print"tctype is $tctype\n";
  return 0;

  system("make crossmake JOBS=$jobs USE_CROSSMAKE=1 ARCH=\"$arch\" MUSLABI=\"$abi\"")
    == 0 || die "failed at make, aborting...";
  chdir "deps-$platform" || die "could not cd";
  system(
    "tar -czf ../tarballs/$platform-$tctype.tgz $platform-$tctype"
  ) == 0 || die "failed to make a tarball";
}

foreach my $platform (@ARGV) {
  chomp $platform;
  if (!is_supported($platform)) {
    die "$platform is not supported!"
  }
}

foreach my $platform (@ARGV) {
  chomp $platform;
  build $platform;
}
