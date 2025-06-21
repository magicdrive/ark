package model

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var ByteStringRegexpString = `(?i)^(\d+(?:\.\d+)?)(B|K|KB|KI|KIB|M|MB|MI|MIB|G|GB|GI|GIB|T|TB|TI|TIB|P|PB|PI|PIB)?$`

var byteUnitMap = map[string]float64{
	"B": 1,
	"K": 1 << 10, "KB": 1 << 10, "KI": 1 << 10, "KIB": 1 << 10,
	"M": 1 << 20, "MB": 1 << 20, "MI": 1 << 20, "MIB": 1 << 20,
	// "G": 1 << 30, "GB": 1 << 30, "GI": 1 << 30, "GIB": 1 << 30,
	// "T": 1 << 40, "TB": 1 << 40, "TI": 1 << 40, "TIB": 1 << 40,
	// "P": 1 << 50, "PB": 1 << 50, "PI": 1 << 50, "PIB": 1 << 50,
}

var ByteStringRegexp = regexp.MustCompile(ByteStringRegexpString)

type ByteString string

func (b *ByteString) Set(value string) error {
	if ByteStringRegexp.MatchString(value) {
		*b = ByteString(value)
		return nil
	}
	return fmt.Errorf("invalid value: %q. Must match pattern %s", value, ByteStringRegexpString)
}

func (b *ByteString) String() string {
	return string(*b)
}

func (b *ByteString) Bytes() (int, error) {
	matches := ByteStringRegexp.FindStringSubmatch(strings.ToUpper(b.String()))
	if len(matches) != 3 {
		return 0, errors.New("invalid byte size format")
	}

	numStr := matches[1] // e.g. "1.5"
	unit := matches[2]   // e.g. "G"
	if unit == "" {
		unit = "B" // default
	}

	multiplier, ok := byteUnitMap[unit]
	if !ok {
		return 0, fmt.Errorf("unsupported unit: %s", unit)
	}

	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number part: %s", numStr)
	}

	return int(num * multiplier), nil
}
