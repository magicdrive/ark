#!/bin/sh
# Prepare the Rust compiler oracle of internal/languages/rust (TestOracle_Compiler).
#
# usage: sh docs/scripts/rust-oracle.sh <crate-archive-dir> <out-dir>
#
#   <crate-archive-dir>  a directory holding the .crate archives listed below
#                        (Cargo keeps them in ~/.cargo/registry/cache/<index>/)
#   <out-dir>            a new, empty directory
#
# then:
#   ARK_RUST_ORACLE=<out-dir> ARK_RUST_ORACLE_SRC=<out-dir>/src \
#     go test ./internal/languages/rust -run TestOracle_Compiler -v
#
# The oracle is rustc's save-analysis: for every reference it compiles, the
# definition it resolves to. save-analysis is an unstable interface: it needs
# RUSTC_BOOTSTRAP=1 and `-Z save-analysis`, and rustc 1.70 removed it — use a
# rustc of 1.69 or earlier (measured with rustc 1.58.0). It is not part of CI.
#
# What runs: rustc compiles each crate's src/lib.rs, nothing else. The corpus
# is fixed — the archives below, verified by SHA-256 before use and unpacked
# into <out-dir> — and limited to crates with no build script and no
# procedural macro, which the script refuses, and no dependency, without
# which a crate does not compile (the script stops): no crate code,
# build script or macro is run, nothing is fetched, Cargo is not used. rustc
# runs with an empty environment (env -i) but for PATH, HOME (a directory of
# <out-dir>) and the Cargo package variables some crates read with env!, and
# by the toolchain's own binary rather than the rustup proxy. A compilation
# compiles for the host and default features only: code another target or
# configuration compiles is not in the oracle (the test counts what it cannot
# verify).
set -eu

[ $# -eq 2 ] || { echo "usage: $0 <crate-archive-dir> <out-dir>" >&2; exit 2; }
archives=$1
out=$2
mkdir -p "$out"
[ -z "$(ls -A "$out")" ] || { echo "$out is not empty" >&2; exit 2; }
out=$(cd "$out" && pwd)
rustc=$(rustc --print sysroot)/bin/rustc
"$rustc" -V > "$out/RUSTC_VERSION"
mkdir -p "$out/src" "$out/home"

while read -r sum crate edition; do
	archive=$archives/$crate.crate
	[ -f "$archive" ] || { echo "missing $archive" >&2; exit 1; }
	got=$(shasum -a 256 "$archive" | cut -d' ' -f1)
	[ "$got" = "$sum" ] || { echo "$crate: SHA-256 $got, want $sum" >&2; exit 1; }
	tar -xzf "$archive" -C "$out/src"
	dir=$out/src/$crate
	if [ -e "$dir/build.rs" ] || grep -q 'proc-macro *= *true' "$dir/Cargo.toml"; then
		echo "$crate: has a build script or is a procedural macro" >&2; exit 1
	fi
	name=$(sed -n 's/^name *= *"\(.*\)"/\1/p' "$dir/Cargo.toml" | head -1 | tr - _)
	version=$(sed -n 's/^version *= *"\(.*\)"/\1/p' "$dir/Cargo.toml" | head -1)
	mkdir -p "$out/$crate"
	( cd "$out/$crate" && env -i PATH=/usr/bin:/bin HOME="$out/home" RUSTC_BOOTSTRAP=1 \
		CARGO_PKG_NAME="$name" CARGO_PKG_VERSION="$version" CARGO_PKG_VERSION_MAJOR=0 \
		CARGO_PKG_VERSION_MINOR=0 CARGO_PKG_VERSION_PATCH=0 CARGO_PKG_AUTHORS= \
		CARGO_PKG_DESCRIPTION= CARGO_PKG_HOMEPAGE= CARGO_PKG_REPOSITORY= CARGO_MANIFEST_DIR="$dir" \
		"$rustc" --edition "$edition" --crate-type lib --crate-name "$name" --cap-lints allow \
		-Z save-analysis "$dir/src/lib.rs" -o "$out/$crate/lib.rlib" > compile.log 2>&1 ) \
		|| { echo "$crate: rustc failed (see $out/$crate/compile.log)" >&2; exit 1; }
done <<'LIST'
5d2e7343e7fc9de883d1b0341e0b13970f764c14101234857d2ddafa1cb1cac2 adler32-1.0.4 2015
6f33b5018f120946c1dcf279194f238a9f146725593ead1c08fa47ff22b0b5d3 ahash-0.2.18 2018
ee49baf6cb617b853aa8d93bf420db2383fab46d314482ca2803b40d5fde979b ansi_term-0.11.0 2015
d7b8a9123b8027467bce0099fe556c628a53c8d83df0507084c31e9ba2e39aff arc-swap-0.4.4 2015
a4c527152e37cf757a3f78aae5a06fbeefdb07ccc535c980a3208ee3060dd544 arrayref-0.3.6 2015
cff77d8686867eceff3105329d4698d96c2391c176d5d03adc90c7389162b5b8 arrayvec-0.5.1 2018
1d49d90015b3c36167a20fe2810c5cd875ad504b39cff3d4eae7977e6b7c1cb2 autocfg-0.1.7 2015
f8aac770f1885fd7e387acedd76065302551364496e46b3dd00860b2f8359b9d autocfg-1.0.0 2015
b41b7ea54a0c9d92199de89e20e58d49f02f8e699814ef3fdf266f6f748d15c7 base64-0.11.0 2018
40e38929add23cdf8a366df9b0e088953150724bcbe5fc330b0d8eb3b328eec8 bufstream-0.1.4 2015
e3b5ca7a04898ad4bcd41c90c5285445ff5b791899bb1b0abdd2a2aa791211d7 byte-tools-0.3.1 2015
130aac562c0dd69c56b3b1cc8ffd2e17be31d0b6c25b61c96b76231aa23e39e1 bytes-0.5.4 2018
61da1bee28b8e078a668885d6424a14fd0ea6a6b2646fdf4e44bda702df3fad8 c2rust-asm-casts-0.1.1 2018
95e28fa049fda1c330bcf9d723be7663a899c4679724b34c81e9f5a326aab8cd cc-1.0.50 2018
4785bdd1c96b2a846b2bd7cc02e86b6b3dbf14e7e53446c4f54c92a361040822 cfg-if-0.1.10 2018
245097e9a4535ee1e3e3931fcfcd55a796a44c643e8596ff6566d68f09b87bbc constant_time_eq-0.1.5 2015
6ff9c56c9fb2a49c05ef0e431485a22400af20d33226dc0764d891d09e724127 copyless-0.1.4 2018
77c90badedccf4105eca100756a0b1289e191f6fcbdadd3cee1d2f614f97da8f dotenv-0.15.0 2018
4358a9e11b9a09cf52383b451b49a169e8d797b68aa02301ff586d70d9661ea3 dtoa-0.4.5 2015
bb1f6b1ce1c140482ea30ddd3335fc0024ac7ee112895426e0a629a6c20adfe3 either-1.5.3 2015
a357d28ed41a50f9c765dbfe56cbc04a64e53e5fc58ba79fbc34c10ef3df831f encode_unicode-0.3.6 2015
f8273f13c977665c5db7eb2b99ae520952fe5ac831ae4cd09d80c4c7042b5ed9 failure-0.1.6 2015
e88a8acf291dafb59c2d96e8f59828f3838bb1a70398823ade51a84de6a6deed fake-simd-0.1.2 2015
f0c77d04ce8edd9cb903932b608268b3fffec4163dc053b3b402bf47eac1f1a8 futures-channel-0.3.4 2018
f25592f769825e89b92358db00d26f965761e094951ac44d3663ef25b7ac464a futures-core-0.3.4 2018
f674f3e1bcb15b37284a90cedf55afdba482ab061c407a9c0ebbd0f3109741ba futures-executor-0.3.4 2018
a638959aa96152c7a4cddf50fcb1e3fede0583b27157c26e67d6f99904090dc6 futures-io-0.3.4 2018
3466821b4bc114d95b087b850a724c6f83115e929bc88f1fa98a3304a944c8a6 futures-sink-0.3.4 2018
7b0a34e53cf6cdcd0178aa573aed466b646eb3db769570841fda0c7ede375a27 futures-task-0.3.4 2018
9b919933a397b79c37e33b77bb2aa3dc8eb6e165ad809e58ff75bc7db2e34574 glob-0.3.0 2015
b8b7a7c0c47db5545ed3fef7468ee7bb5b74691498139e4b3f6a20685dc6dd8e itoa-0.4.5 2015
a91d884b6667cd606bb5a69aa0c99ba811a115fc68915e7056ec08a46e93199a language-tags-0.2.2 2015
e2abad23fbc42b3700f2f279844dc832adb2b2eb069b2df918f455c4e18cc646 lazy_static-1.4.0 2015
ae91b68aebc4ddb91978b11a1b02ddd8602a05ec19002801c5666000e05e0f83 linked-hash-map-0.5.2 2015
0609345ddee5badacf857d4f547e0e5a2e987db77085c24cd887f73573a04237 lru-0.4.3 2015
ffbee8634e0d45d258acb448e7eaab3fce7a0a467395d4d9f228e3c1f01fb2e4 match_cfg-0.1.0 2015
2a60c7ce501c71e03a9c9c0d35b861413ae925bd979cc7a4e30d060069aaac8d mime-0.3.16 2015
72ef4a56884ca558e5ddb05a1d1e7e1bfd9a68d9ed024c21704cc98872dae1bb nodrop-0.1.14 2015
2839e79665f131bdb5782e51f2c6c9599c133c6098982a54c794358bf432529c opaque-debug-0.2.3 2015
237844750cfbb86f67afe27eee600dfbbcb6188d734139b534cbfbf4f96792ae pin-project-lite-0.1.4 2018
5894c618ce612a3fa23881b152b608bafb8c56cfc22f434a3ba3120b40f7b587 pin-utils-0.1.0-alpha.4 2018
05da548ad6865900e60eaba7f589cc0783590a92e940c26953ff81ddbab2d677 pkg-config-0.3.17 2015
74490b50b9fbe561ac330df47c08f3f33073d2d00c150f719147d7c54522fa1b ppv-lite86-0.2.6 2018
a1d01941d82fa2ab50be1e79e6714289dd7cde78eba4c074bc5a4374f650dfe0 quick-error-1.2.3 2015
90bde5296fc891b0cef12a6d03ddccc162ce7b2aff54160af9338f8d40df6d19 rand_core-0.5.1 2018
b28dfe3fe9badec5dbf0a79a9cccad2cfc2ab5484bdb3e44cbd1ae8b3ba2be06 regex-syntax-0.6.14 2015
1132f845907680735a84409c3bebc64d1364a5683ffbce899550cd09d5eaefc1 regex-syntax-0.6.16 2015
4a83fa3702a688b9359eccba92d153ac33fd2e8462f9e0e3fdf155239ea7792e remove_dir_all-0.5.2 2015
4c691c0e608126e00913e33f0ccf3727d5fc84573623b8d65b2df340b5201783 rustc-demangle-0.1.16 2015
93fc1dc3aaa9bfed95e02e6eadabb4baf7e3078b0bd1b4d7b6b0b68378900502 same-file-1.0.6 2018
d29ab0c6d3fc0ee92fe66e2d99f700eab17a8d57d1c1d3b748380fb20baa78cd scopeguard-1.1.0 2015
388a1df253eca08550bef6c72392cfe7c30914bf41df5269b68cbd6ff8f570a3 semver-parser-0.7.0 2015
2579985fda508104f7587689507983eadd6a6e84dd35d6d115361f530916fa0d sha1-0.6.0 2015
c111b5bd5695e56cffe5129854aa230b39c93a305372fdbb2668ca2394eea9f8 slab-0.4.2 2015
6e63cff320ae2c57904679ba7cb63280a3dc4613885beafb148ee7bf9aa9042d spin-0.5.2 2015
7f3eb36b47e512f8f1c9e3d10c2c1965bc992bd9cdb024fa581e2194501c83d3 static_assertions-0.3.4 2015
8ea5119cdb4c55b55d432abb513a0429384878c15dde60cc77b1c99de1a95a6a strsim-0.8.0 2015
bb6bfa289a4d7c5766392812c0a1f4c1ba45afa1ad47803c11e1f407d846d75f termcolor-1.1.0 2018
571da51182ec208780505a32528fc5512a8fe1443ab960b3f2f3ef093cd16930 tokio-util-0.2.0 2018
3bfd5b7557925ce778ff9b9ef90e3ade34c524b5ff10e239c69a42d546d2af56 twox-hash-1.5.0 2018
e83e153d1053cbb5a118eeff7fd5be06ed99153f00dbcd8ae310c5fb2b22edc0 unicode-segmentation-1.6.0 2015
caaa9d531767d1ff2150b9332433f32a24622147e5ebb1f26409d5da67afd479 unicode-width-0.1.7 2015
fc72304796d0818e357ead4e000d19c9c174ab23dc11093ac919054d20a6a7fc unicode-xid-0.1.0 2015
826e7639553986605ec5979c7dd957c7895e93eabed50ab2ffa7f6128a75097c unicode-xid-0.2.0 2015
9fde2f6a4bea1d6e007c4ad38c6839fa71cbb63b6dbf5b595aa38dc9b1093c11 uuid-0.8.1 2018
05c78687fb1a80548ae3250346c3db86a80a7cdd77bda190189f2d0a0987c81a vec_map-0.8.1 2015
914b1a6776c4c929a602fafd8bc742e06365d4bcbe48c30f9cca5824f70dc9dd version_check-0.1.5 2015
078775d0255232fb988e6fccf26ddc9d1ac274299aaedcedce21c6f72cc533ce version_check-0.9.1 2015
6a02e4885ed3bc0f2de90ea6dd45ebcbb66dacffe03547fadbb0eeae2770887d void-1.0.2 2015
cccddf32554fecc6acb585f82a32a72e28b48f8c4c1883ddfeeeaa96f7d8e519 wasi-0.9.0+wasi-snapshot-preview1 2018
4ccfbf554c6ad11084fb7517daca16cfdcaccbdadba4fc336f032a8b12c2ad80 winapi-util-0.1.3 2018
LIST
