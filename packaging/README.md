# Release packaging

v0.1.0 is published as an experimental prerelease at https://github.com/NamcheAI/searcli/releases/tag/v0.1.0. Live Google OAuth/API interoperability remains untested.

The release workflow builds macOS arm64/amd64 archives and computes checksums.txt from the exact files. Downloaded uploaded assets were verified with shasum -a 256 -c checksums.txt; arm64 version smoke passed.

Homebrew: `brew install namcheai/tap/searcli`. Formula repository: https://github.com/NamcheAI/homebrew-tap. The formula installs prebuilt binaries, not source. Installation, brew test and brew audit --strict passed on macOS arm64. amd64 was built and checksummed but not executed locally.

For future releases: review changes, pass CI, push an approved version tag, inspect the draft release and downloaded asset hashes, then publish with accurate validation status. Substitute the real hashes and version in searcli.rb.in and narrowly update Formula/searcli.rb in the tap. Never fabricate hashes or overwrite unrelated tap contents.
