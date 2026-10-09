package mcp

import (
	"log"
	"sync"

	"github.com/magicdrive/ark/internal/commandline"
)

// Secret masking settings (see sanitize.go for what masking does).
//
//	no setting / --mask-secrets on      masking on
//	--mask-secrets off                  masking off for every response (warning at startup)
//
// The setting belongs to whoever starts the server. A request cannot change
// it: the maskSecrets argument of get_file_content and get_files_arklite is
// accepted and has no effect, the contract
// TestFileContent_SecurityOverridesStillIgnored pins — an agent, or text in
// the repository steering it, must not be able to unmask what the operator
// chose to mask. Masking is independent of the .arkignore access policy:
// turning it off never makes an excluded file readable.

// maskingWarning is logged (stderr; never the stdio protocol stream) when the
// server setting turns masking off. It names no file and no content.
const maskingWarning = "WARNING: MCP secret masking is disabled. Source content may contain credentials or other sensitive information."

// warnedMaskingOff deduplicates the warning.
var warnedMaskingOff sync.Once

// masksByDefault reports the server setting: on unless --mask-secrets off.
// An option without the setting (an empty value) masks.
func masksByDefault(opt *commandline.Option) bool {
	return opt == nil || opt.MaskSecretsFlagValue != "off"
}

// warnIfServerMaskingOff logs the warning, once per process, for a server
// started with --mask-secrets off.
func warnIfServerMaskingOff(opt *commandline.Option) {
	if !masksByDefault(opt) {
		warnedMaskingOff.Do(func() { log.Printf("ark: %s (ark mcp-server --mask-secrets off)", maskingWarning) })
	}
}

// masking reports whether a tool call's result is masked: the server setting.
func (h *ToolsHandler) masking(string, map[string]interface{}) bool {
	return masksByDefault(h.opt)
}
