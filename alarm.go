package main

import (
	"fmt"
	"strings"

	"github.com/gosnmp/gosnmp"
)

type Alarm struct {
	Alarms []AlarmEntry
	Snmp   *SNMP
	Trap   []TrapData

	NeedApply bool
}

func (a *Alarm) SetSNMP(snmp *SNMP) {
	a.Snmp = snmp
}

func (a *Alarm) AddTrap(add bool, index int, oid string) {
	name := "upsTrapAlarmEntryAdded"
	if !add {
		name = "upsTrapAlarmEntryRemoved"
	}
	a.Trap = append(a.Trap, TrapData{
		OID: name,
		Data: []TrapDataItem{
			{
				OID:   "upsAlarmId",
				Type:  gosnmp.Integer,
				Value: index,
			},
			{
				OID:   "upsAlarmDescr",
				Type:  gosnmp.ObjectIdentifier,
				Value: oid,
			},
		},
	})
}

func (a *Alarm) Add(desc string) int {
	if !strings.HasPrefix(desc, ".") {
		oid := a.Snmp.GetOID(desc, -1)
		if oid == "" {
			fmt.Printf("%s not found", desc)
			panic(desc + " not found")
		}
		desc = oid
	}

	index := len(a.Alarms)
	a.AddAlarmEntry(AlarmEntry{
		Index: index,
		Descr: desc,
		Time:  TimesTamp(getRunningTimeInSeconds()),
	})
	a.AddTrap(true, index, desc)
	return index
}

func (a *Alarm) AddAlarmEntry(entry AlarmEntry) {
	a.Alarms = append(a.Alarms, entry)
	a.NeedApply = true
}

func (a *Alarm) Clear() {
	a.Alarms = a.Alarms[:0]
	a.NeedApply = true
}

func (a *Alarm) Remove(index int) {
	if index < len(a.Alarms) {
		a.AddTrap(false, a.Alarms[index].Index, a.Alarms[index].Descr)
		a.Alarms = append(a.Alarms[:index], a.Alarms[index+1:]...)
		a.NeedApply = true
	}
}

func (a *Alarm) getOID(desc string) string {
	if strings.HasPrefix(desc, ".") {
		return desc
	}
	oid := a.Snmp.GetOID(desc, -1)
	if oid == "" {
		Logger.Errorf("%s not found", desc)
		return ""
	}
	return oid
}

func (a *Alarm) RemoveWithDesc(desc string) (int, bool) {
	desc = a.getOID(desc)
	for i, alarm := range a.Alarms {
		if alarm.Descr == desc {
			a.Remove(i)
			a.NeedApply = true
			a.AddTrap(false, alarm.Index, alarm.Descr)
			return alarm.Index, true
		}
	}
	return -1, false
}

func (a *Alarm) Exist(desc string) bool {
	desc = a.getOID(desc)
	for _, alarm := range a.Alarms {
		if alarm.Descr == desc {
			return true
		}
	}
	return false
}

func (a *Alarm) Apply() {
	if !a.NeedApply {
		return
	}
	a.NeedApply = false
	a.Snmp.RemoveAllTable("upsAlarmId")
	a.Snmp.RemoveAllTable("upsAlarmDescr")
	a.Snmp.RemoveAllTable("upsAlarmTime")
	size := len(a.Alarms)
	a.Snmp.Data.Alarm.Present = size

	if size == 0 {
		return
	}

	onGet := func(obj any, index int) (any, error) {
		if index >= size {
			return nil, nil
		}
		switch obj.(string) {
		case "upsAlarmId":
			return a.Alarms[index].Index, nil
		case "upsAlarmDescr":
			return a.Alarms[index].Descr, nil
		case "upsAlarmTime":
			return a.Alarms[index].Time, nil
		}
		return nil, nil
	}
	a.Snmp.AddTable("upsAlarmId", "upsAlarmId", size, gosnmp.Integer, onGet)
	a.Snmp.AddTable("upsAlarmDescr", "upsAlarmDescr", size, gosnmp.ObjectIdentifier, onGet)
	a.Snmp.AddTable("upsAlarmTime", "upsAlarmTime", size, gosnmp.TimeTicks, onGet)
	a.Snmp.Apply()
	for _, trap := range a.Trap {
		a.Snmp.SendTrap(trap)
	}
	a.Trap = a.Trap[:0]
}
