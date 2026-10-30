// Package installer holds the tests of install.sh, the release installer at
// the root of the repository. It has no code of its own.
//
// The tests run install.sh, unchanged, against a fake github.com: a local TLS
// server reached through a local HTTPS proxy (https_proxy) with a test CA
// (CURL_CA_BUNDLE, SSL_CERT_FILE, WGETRC). The releases it serves are built by
// the tests, so they need no network.
package installer
