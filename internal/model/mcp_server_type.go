package model

import (
	"fmt"
)

const (
	TypeHttp  = "http"
	TypeStdio = "stdio"
)

var McpServerTypeUnitMap = map[string]string{
	"http":  TypeHttp,
	"stdio": TypeStdio,
}

type McpSreverType string

func (m *McpSreverType) Set(value string) error {
	if unit, ok := McpServerTypeUnitMap[value]; ok {
		*m = McpSreverType(unit)
		return nil
	} else {
		return fmt.Errorf("invalid value: %q. Allowed values are 'http', 'stdio'", value)
	}
}

func (m *McpSreverType) String() string {
	return string(*m)
}
