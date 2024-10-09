package main

import (
	"errors"
	"math"

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
		Voltage   int
		Current   int
		Frequency int
		Power     int
	}
	OutputInfo struct {
		Voltage int
		Current int
		Power   int
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
		Logger.Debugf("QueryResult: %#v", v)
		// Battery
		data.Battery.Voltage = int(math.Round(float64(v.BatteryVoltage) * 10.0))
		rating := userData.Rating

		batteryMax := 27.4
		batteryLow := 21.6
		charge := (float64(v.BatteryVoltage) - batteryLow) / (batteryMax - batteryLow) * 100
		data.Battery.Charge = int(math.Round(charge))
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

		data.Battery.Temp = int(math.Round(float64(v.Temperature)))

		// 电流 = (电流百分比 / 100) * 额定电流
		current := (float64(v.OPCurrentPercent) / 100) * float64(rating.CurrentRating)
		// 电池电流 = (电流 * 输出电压) / 电池电压
		batteryCurrent := (current * float64(v.OPVoltage)) / float64(v.BatteryVoltage)

		// // 计算剩余时间 (秒)
		// // 剩余容量 Ah = (Battery.Charge / 100) * 额定电池容量 (7)
		// // 放电时间 (小时) = 剩余容量 / 当前电流
		// if current > 0 { // 避免除以0
		// 	remainingCapacity := (charge / 100) * 7
		// 	dischargeTimeHours := remainingCapacity / current
		// 	data.Battery.Minutes = int(dischargeTimeHours * 60) // 转换为分钟
		// } else {
		// 	data.Battery.Minutes = 60 // 如果没有电流，无法计算放电时间
		// }

		// if data.Battery.Minutes > 60 {
		// 	data.Battery.Minutes = 60
		// }

		// MT1000-Pro
		// 50%  负载 10  分钟
		// 100% 负载 3.5 分钟
		t50 := 10.0
		t100 := 3.5

		// 使用线性插值计算时间
		time := t50 + (t100-t50)/(100.0-50.0)*(float64(v.OPCurrentPercent)-50.0)

		data.Battery.Minutes = int(math.Round(time))

		Logger.Debugf("Battery: %fV %f%% %dC %fA %fM", v.BatteryVoltage, charge, data.Battery.Temp, current, time)

		// Output
		data.Output.Freq = int(math.Round(float64(v.IPFreq) * 10.0))
		data.Bypass.Freq = int(math.Round(float64(v.IPFreq) * 10.0))
		if v.Status.UtilityFail {
			data.Output.Source = 5

			data.Input.LineBads = 1

			userData.BatterySecond += 1

			data.Battery.Current = int(math.Round(batteryCurrent * 10.0))

			if !alarm.Exist("upsAlarmInputBad") {
				alarm.Add("upsAlarmInputBad")
			}
		} else {
			data.Output.Source = 3

			data.Input.LineBads = 0

			userData.BatterySecond = 0

			data.Battery.Current = 0

			alarm.RemoveWithDesc("upsAlarmInputBad")
		}
		userData.OutputInfo.Voltage = int(v.OPVoltage)
		userData.OutputInfo.Current = int(current * 10.0)
		userData.OutputInfo.Power = int(float64(v.OPVoltage) * current)
		userData.OutputInfo.Load = v.OPCurrentPercent

		// Input
		userData.InputInfo.Voltage = int(v.IPVoltage)
		userData.InputInfo.Current = int(float64(current) * 10.0)
		userData.InputInfo.Frequency = int(v.IPFreq * 10.0)
		userData.InputInfo.Power = int(float64(v.OPVoltage) * current)

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
		Logger.Debugf("RatingInfo: %#v", v)

		userData.Rating = v
	default:
		Logger.Debugf("Default: %#v", v)
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

	data.Bypass.NumLines = 1

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

		case "upsOutputLineIndex", "upsBypassLineIndex":
			return 1, nil
		case "upsOutputVoltage", "upsBypassVoltage":
			return data.OutputInfo.Voltage, nil
		case "upsOutputCurrent", "upsBypassCurrent":
			return data.OutputInfo.Current, nil
		case "upsOutputPower", "upsBypassPower":
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

	snmp.AddTable("upsBypassLineIndex", "upsBypassLineIndex", 1, gosnmp.Integer, onGet)
	snmp.AddTable("upsBypassVoltage", "upsBypassVoltage", 1, gosnmp.Integer, onGet)
	snmp.AddTable("upsBypassCurrent", "upsBypassCurrent", 1, gosnmp.Integer, onGet)
	snmp.AddTable("upsBypassPower", "upsBypassPower", 1, gosnmp.Integer, onGet)

	snmp.Apply()

	return nil
}

func Mt1000ProSetCallback(snmp *SNMP, name string, value any) error {
	data := snmp.Data
	Logger.Debugf("SetCallback: %s=%v", name, value)
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
		Bypass: &SNMPDataBypass{
			NumLines: 1,
			Freq:     1,
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

	SetCallback: Mt1000ProSetCallback,

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
