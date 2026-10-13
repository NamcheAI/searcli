# Publication checklist (not executed)

1. Obtain independent security/correctness review; fix findings and rerun CI. Live OAuth/API smoke testing needs the user's Desktop credentials and property.
2. Confirm repository ownership and public visibility. Authenticated identity observed: jodok; active admin membership in namcheai. namcheai/searcli and NamcheAI/homebrew-tap could not be resolved during implementation. Membership in namche returned HTTP 404; a follow-up lookup confirms namche is a GitHub User, not an organization. NamcheAI reports members_can_create_repositories=true. This does not prove those names are available or authorize creation in namche.
3. After review, create/push the approved public namcheai/searcli repository. Inspect existing repositories before modifying them. Confirm MIT licensing with owner before publication.
4. Push an approved semantic version tag (e.g. v0.1.0). The workflow creates a **draft** release with two macOS archives and checksums.txt computed from those exact archives. Verify downloads/checksums and smoke-test both architectures, then publish the draft.
5. Resolve write permission to NamcheAI/homebrew-tap (Homebrew namcheai/tap). Substitute @VERSION@ (without v) and the actual archive hashes from the verified release into searcli.rb.in; put the resulting formula at Formula/searcli.rb in that tap. Never publish this unfilled template or invent hashes.
6. Run brew audit --strict and brew install namcheai/tap/searcli, then brew test namcheai/tap/searcli on supported macOS architectures; publish only after successful checks.

No GitHub repository, tag, release, formula, scheduler or Google grant was created by this implementation task.

The formula installs prebuilt release binaries; it does not compile source.
