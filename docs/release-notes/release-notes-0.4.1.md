0.4.1 Release Notes
===============================

Lightwalletd version 0.4.1 is now available from:

  <https://github.com/zcash/lightwalletd/releases/tag/v0.4.1>

Or cloned from:

  <https://github.com/zcash/lightwalletd/tree/v0.4.1>

Lightwalletd must be built from source code (there are no binary releases
at this time).

This minor release includes various bug fixes, performance
improvements, and test code improvements.

Please report bugs using the issue tracker at GitHub:

  <https://github.com/zcash/lightwalletd/issues>

How to Upgrade
==============

If you are running an older version, shut it down. Run `make` to generate
the `./lightwalletd` executable. Run `./lightwalletd version` to verify
that you're running the correct version (v0.4.0). Some of the command-line
arguments (options) have changed since the previous release; please
run `./lightwalletd help` to view them.

Compatibility
==============

Lightwalletd is supported and extensively tested on operating systems using
the Linux kernel, and to a lesser degree macOS. It is not recommended
to use Lightwalletd on unsupported systems.

0.4.1 change log
=================

### Infrastructure
- #161 Add docker-compose
- #227 Added tekton for Docker image build
- #236 Add http endpoint and prometheus metrics framework

### Tests and QA
- #234 darksidewalletd

### Documentation
- #107 Reorg documents for updates and upcoming new details
- #188 add documentation for lightwalletd APIs and data types
- #195 add simple gRPC test client
- #270 add issue and PR templates

Credits
=======

Thanks to everyone who directly contributed to this release:

- adityapk00
- Marshall Gaucher
- Kevin Gorhan
- Taylor Hornby
- Linda Lee
- Brad Miller
- Charlie O'Keefe
- Larry Ruane
- Za Wilcox
- Ben Wilson
