package main

import (
	"errors"
	"fmt"

	"github.com/apex/log"
	"github.com/gosnmp/gosnmp"
)

type Device struct {
	InitCallback  func(snmp *SNMP, data *SNMPData) error
	EnableService SNMPData

	GetInfo         string // Q1
	GetRated        string // F
	GetManufacturer string // I

	OnReceive   func(snmp *SNMP, data *SNMPData, value string) error
	SetCallback func(snmp *SNMP, name string, value any) error

	Test             string // T
	TestToBatteryLow string // TL
	TestWithMinimum  string // T<m>

	Poweroff             string // S<m>
	PoweroffAndStart     string // S<m>R<m2>
	PoweroffAndStartWith string // S<m>R<m2> <m>分钟后关机<m2>后启动

	SwitchBuzz string // Q

	CancelAllPoweroff string // C
	CancelAllTest     string // CT

	// 三进三出 UPS
	ExtraGetInfo   string // G1
	ExtraGetError  string // G2
	ExtraGetTPInfo string // G3
	ExtraGetRated  string // GF
}

type Mt1000ProUserData struct {
	Rating        RatingInfo
	BatterySecond int
	InputInfo     struct {
		Voltage   float32
		Current   float32
		Frequency float32
		Power     float32
	}
	OutputInfo struct {
		Voltage float32
		Current float32
		Power   float32
		Load    int
	}
}

func Mt1000ProOnReceive(snmp *SNMP, data *SNMPData, value string) error {
	parse := ProtoParse(value)
	if parse == nil {
		return errors.New("parse error: " + value)
	}

	userData := data.UserData.(*Mt1000ProUserData)

	switch v := parse.(type) {
	case QueryResult:
		log.Debugf("QueryResult: %#v", v)
		// Battery
		data.Battery.Voltage = int(v.BatteryVoltage)
		rating := userData.Rating
		charge := (v.BatteryVoltage / rating.BatteryVoltage) * 100
		data.Battery.Charge = int(charge)
		if data.Battery.Charge > 100 {
			data.Battery.Charge = 100
		}

		if v.Status.BatteryLow {
			data.Battery.Status = 3
			if !alarm.Exist("upsAlarmLowBattery") {
				alarm.Add("upsAlarmLowBattery")
			}
		} else {
			alarm.RemoveWithDesc("upsAlarmLowBattery")
			data.Battery.Status = 2
		}

		data.Battery.Temp = int(v.Temperature)

		current := (float32(v.OPCurrentPercent) / 100) * float32(rating.CurrentRating)
		data.Battery.Current = int(current)

		// 计算剩余时间 (秒)
		// 剩余容量 Ah = (Battery.Charge / 100) * 额定电池容量 (7)
		// 放电时间 (小时) = 剩余容量 / 当前电流
		if current > 0 { // 避免除以0
			remainingCapacity := (charge / 100) * 7
			dischargeTimeHours := remainingCapacity / current
			data.Battery.Minutes = int(dischargeTimeHours * 60) // 转换为分钟
		} else {
			data.Battery.Minutes = 60 // 如果没有电流，无法计算放电时间
		}

		if data.Battery.Minutes > 60 {
			data.Battery.Minutes = 60
		}

		// Output
		data.Output.Freq = int(v.IPFreq)
		if v.Status.UtilityFail {
			data.Output.Source = 5

			data.Input.LineBads = 1

			userData.BatterySecond += 1

			if !alarm.Exist("upsAlarmInputBad") {
				alarm.Add("upsAlarmInputBad")
			}
		} else {
			data.Output.Source = 3

			data.Input.LineBads = 0

			userData.BatterySecond = 0

			alarm.RemoveWithDesc("upsAlarmInputBad")
		}
		userData.OutputInfo.Voltage = v.OPVoltage
		userData.OutputInfo.Current = current
		userData.OutputInfo.Power = v.OPVoltage * current
		userData.OutputInfo.Load = v.OPCurrentPercent

		// Input
		userData.InputInfo.Voltage = v.IPVoltage
		userData.InputInfo.Current = current
		userData.InputInfo.Frequency = v.IPFreq
		userData.InputInfo.Power = v.OPVoltage * current

		// Config
		if v.Status.BuzzerActive {
			data.Config.AudibleStatus = 2
		} else {
			data.Config.AudibleStatus = 3
		}

		// Alarm
		if v.Status.ShutdownActive {
			if !alarm.Exist("upsAlarmUpsSystemOff") {
				alarm.Add("upsAlarmUpsSystemOff")
			}
		} else {
			alarm.RemoveWithDesc("upsAlarmUpsSystemOff")
		}

		if v.Status.UPSFailed {
			if !alarm.Exist("upsAlarmGeneralFault") {
				alarm.Add("upsAlarmGeneralFault")
			}
		} else {
			alarm.RemoveWithDesc("upsAlarmGeneralFault")
		}

		if v.OPCurrentPercent > 120 {
			if !alarm.Exist("upsAlarmOutputOverload") {
				alarm.Add("upsAlarmOutputOverload")
			}
		} else {
			alarm.RemoveWithDesc("upsAlarmOutputOverload")
		}

		if v.Status.BuzzerActive {
			if config.DisableBuzz {
				snmp.TtySend(snmp.Device.SwitchBuzz)
			}
		}

		alarm.Apply()
	case RatingInfo:
		log.Debugf("RatingInfo: %#v", v)

		userData.Rating = v
	default:
		log.Debugf("default: %#v", v)
	}

	return nil
}

func Mt1000ProInit(snmp *SNMP, data *SNMPData) error {
	data.Ident.Manufacturer = "Eaton"
	data.Ident.Model = "MT1000-Pro"
	data.Ident.SoftwareVersion = "1.0.0"
	data.Ident.AgentVersion = "1.0.0"

	data.Input.NumLines = 1

	data.Output.NumLines = 1

	data.UserData = &Mt1000ProUserData{}

	onGet := func(obj any, index int) (any, error) {
		name := obj.(string)
		data := data.UserData.(*Mt1000ProUserData)
		switch name {
		case "upsInputLineIndex":
			return 1, nil
		case "upsInputFrequency":
			return data.InputInfo.Frequency, nil
		case "upsInputVoltage":
			return data.InputInfo.Voltage, nil
		case "upsInputCurrent":
			return data.InputInfo.Current, nil
		case "upsInputTruePower":
			return data.InputInfo.Power, nil

		case "upsOutputLineIndex":
			return 1, nil
		case "upsOutputVoltage":
			return data.OutputInfo.Voltage, nil
		case "upsOutputCurrent":
			return data.OutputInfo.Current, nil
		case "upsOutputPower":
			return data.OutputInfo.Power, nil
		case "upsOutputPercentLoad":
			return data.OutputInfo.Load, nil
		}
		return nil, errors.New("not found")
	}

	snmp.AddTable("upsInputLineIndex", "upsInputLineIndex", 1, gosnmp.Integer, onGet)
	snmp.AddTable("upsInputFrequency", "upsInputFrequency", 1, gosnmp.Integer, onGet)
	snmp.AddTable("upsInputVoltage", "upsInputVoltage", 1, gosnmp.Integer, onGet)
	snmp.AddTable("upsInputCurrent", "upsInputCurrent", 1, gosnmp.Integer, onGet)
	snmp.AddTable("upsInputTruePower", "upsInputTruePower", 1, gosnmp.Integer, onGet)

	snmp.AddTable("upsOutputLineIndex", "upsOutputLineIndex", 1, gosnmp.Integer, onGet)
	snmp.AddTable("upsOutputVoltage", "upsOutputVoltage", 1, gosnmp.Integer, onGet)
	snmp.AddTable("upsOutputCurrent", "upsOutputCurrent", 1, gosnmp.Integer, onGet)
	snmp.AddTable("upsOutputPower", "upsOutputPower", 1, gosnmp.Integer, onGet)
	snmp.AddTable("upsOutputPercentLoad", "upsOutputPercentLoad", 1, gosnmp.Integer, onGet)

	return nil
}

func Mt1000ProSetCallback(snmp *SNMP, name string, value any) error {
	data := snmp.Data
	fmt.Printf("SetCallback: %s, %v\n", name, value)
	switch name {
	case "upsConfigAudibleStatus":
		if value == 1 || value == 3 {
			if data.Config.AudibleStatus == 1 {
				snmp.TtySend(snmp.Device.SwitchBuzz)
			}
		} else {
			if data.Config.AudibleStatus == 0 {
				snmp.TtySend(snmp.Device.SwitchBuzz)
			}
		}
	}
	return nil
}

var Mt1000Pro = Device{
	InitCallback: Mt1000ProInit,

	EnableService: SNMPData{
		Ident: &SNMPDataIdent{
			Manufacturer:    "1",
			Model:           "1",
			SoftwareVersion: "1",
			AgentVersion:    "1",
		},
		Battery: &SNMPDataBattery{
			Status:  1,
			Seconds: 1,
			Minutes: 1,
			Charge:  1,
			Voltage: 1,
			Current: 1,
			Temp:    1,
		},
		Input: &SNMPDataInput{
			NumLines: 1,
			LineBads: 1,
		},
		Output: &SNMPDataOutput{
			Source:   1,
			Freq:     1,
			NumLines: 1,
		},
		Alarm: &SNMPDataAlarm{
			Present: 1,
		},
		Config: &SNMPDataConfig{
			AudibleStatus: 1,
		},
	},

	GetInfo:         "Q1",
	GetRated:        "F",
	GetManufacturer: "",

	OnReceive: Mt1000ProOnReceive,

	Test:             "T",
	TestToBatteryLow: "",
	TestWithMinimum:  "",
	Poweroff:         "",
	PoweroffAndStart: "",

	SwitchBuzz: "Q",

	CancelAllPoweroff: "",
	CancelAllTest:     "",

	ExtraGetInfo:   "",
	ExtraGetError:  "",
	ExtraGetTPInfo: "",
}
