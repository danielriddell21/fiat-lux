// letsgo.mod

// The module is fiat-lux but the binary and every published asset are fiatlux,
// so the project name is set rather than inferred from the module path.
project fiatlux

// The default matrix is five targets; windows/arm64 is the sixth that the
// GoReleaser config built, kept so the published asset list does not shrink.
build (
	linux/amd64
	linux/arm64
	darwin/amd64
	darwin/arm64
	windows/amd64
	windows/arm64
)

brew danielriddell21/tap

// Named explicitly: the image has always been ghcr.io/danielriddell21/fiat-lux,
// which is the repository rather than the project.
image ghcr.io/danielriddell21/fiat-lux

// The base the deleted Dockerfile used; it carries the CA certificates the
// binary needs to make HTTPS calls, which scratch does not. Pinned by digest so
// two releases of one commit cannot differ — bump it deliberately for base fixes.
image base gcr.io/distroless/static-debian12@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

// Releases were marked as pre-releases by the shared workflow after the fact;
// letsgo does it as part of publishing, so promotion is still a manual step.
release prerelease=true
