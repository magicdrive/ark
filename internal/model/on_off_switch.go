package model

import (
	"fmt"
)

const (
	SwitchOn  = "on"
	SwitchOff = "off"
)

var onOffUnitMap = map[string]string{
	"on":  SwitchOn,
	"ON":  SwitchOn,
	"YES": SwitchOn,
	"yes": SwitchOn,
	"y":   SwitchOn,
	"off": SwitchOff,
	"OFF": SwitchOff,
	"NO":  SwitchOff,
	"no":  SwitchOff,
	"n":   SwitchOff,
}

type OnOffSwitch string

func Bool2OnOffSwitch(b bool) OnOffSwitch {
	var result OnOffSwitch
	if b {
		result = "on"
	} else {
		result = "off"
	}
	return result
}

func (m *OnOffSwitch) Set(value string) error {
	if unit, ok := onOffUnitMap[value]; ok {
		*m = OnOffSwitch(unit)
		return nil
	} else {
		return fmt.Errorf("invalid value: %q. Allowed values are 'on', 'off'", value)

	}
}

func (m *OnOffSwitch) Bool() bool {
	value := m.String()
	switch value {
	case SwitchOn:
		return true
	case SwitchOff:
		return false
	default:
		panic(fmt.Sprintf("invalid value: %q. Allowed values are 'on', 'off'", value))
	}
}

func (m *OnOffSwitch) String() string {
	return string(*m)
}
