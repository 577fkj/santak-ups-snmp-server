package main

import (
	"strconv"
	"strings"
)

const QueryByteChar = '('
const RatingByteChar = '#'
const ExtraQueryByteChar = '!'
const EndByteChar = '\r'

// 无效命令和信息的处理:
// 	收到无效的命令时,UPS 要将受到的内容原样返回。若命令 UPS 无法返回信息,则返回“@”

// UPS 开关量状态:<U>, <U>是以二进制数位表示法:<b7b6b5b4b3b2b1b0>, 并以 ASCII 码单位传输的一个状态量
// b7:1 表示 市电电压异常
// b6:1 表示 电池低电压
// b5:1 表示 Bypass 或 Buck Active
// b4:1 表示 UPS 故障
// b3:1 表示 UPS 为后备式(0 表示在线式)
// b2:1 表示 测试中
// b1:1 表示 关机有效
// b0:1 表示 蜂鸣器开
type UPSStatus struct {
	UtilityFail       bool // 市电电压异常, utility power 市电，1 fail 表示 UPS 市电断了，电池供电
	BatteryLow        bool // 电池低电压, 1 low
	BypassBoostActive bool // Bypass 或 Buck Active (1 : AVR 0:NORMAL) 当电网电压变化时，通过自动调整实现输出电压稳定并供给负载使用，称为AVR(AutomaticVoltageRegulation)
	UPSFailed         bool // UPS 故障, 1 failed
	UPSType           bool // 表示 UPS 为后备式(0 表示在线式), 1 standby 0 online
	TestActive        bool // 表示 测试中, 1 test in progress
	ShutdownActive    bool // 表示 关机有效, 1 shutdown active
	BuzzerActive      bool // 表示 蜂鸣器开
}

// UPS 状态查询请求 Q1
// (228.0 228.0 228.4 006 50.2 27.4 25.0 00001000
// (228.0 228.0 228.4 017 50.0 27.4 25.0 00001001
type QueryResult struct {
	IPVoltage      float32 // 输入电压(I/P voltage):MMM.M, M 为0~9的整数，状态量单位为 Vac
	IPFaultVoltage float32 // 输入故障电压(I/P fault voltage):NNN.N, N 为 0~9 的整数,状态量单位为 Vac
	// ** 对后备式 UPS 而言 **
	// 目的是为了标识引起后备式 UPS 转入逆变模式的瞬间毛刺电压。如有电压
	// 瞬变发生,输入电压将在电压瞬变前、后一个查询保持正常。 I/P 异常电压将把瞬
	// 变电压保持到下一个查询。查询完成后,I/P 异常电压将与 I/P 电压保持一致,直
	// 到发生新的瞬变。
	// ** 对在线式 UPS 而言 **
	// 目的是为了标识引起在线式 UPS 转入电池供电模式的短时输入异常。如有
	// 电压瞬变发生,输入电压将在电压瞬变前、后一个查询保持正常。 I/P 异常电压将
	// 把瞬变电压保持到下一个查询。查询完成后,I/P 异常电压将与 I/P 电压保持一致
	// 直到发生新的瞬变。
	OPVoltage        float32 // 输出电压(O/P voltage):PPP.P, P 为 0~9 的整数,状态量单位为 Vac
	OPCurrentPercent int     // 输出电流(O/P current):QQQ, QQQ 是一个相对于最大允许电流的百分比,不是一个绝对值
	IPFreq           float32 // 输入频率(I/P frequency):RR.R, R 为 0~9 的整数,状态量单位为 Hz
	BatteryVoltage   float32 // 电池电压(Battery voltage):SS.S 或 S.SS, S 为 0~9 的整数
	// 对在线式单体电池电压显示方式为 S.SS Vdc
	// 对后备式总电池电压显示方式为 SS.S Vdc
	// ( UPS 类型将在 UPS 状态信息中获得 )
	Temperature float32   // 环境温度(Temperature):TT.T, T 为 0~9 的整数,单位为 C
	Status      UPSStatus // UPS 开关量状态:<U>, <U>是以二进制数位表示法:<b7b6b5b4b3b2b1b0>,
}

// UPS 额定值信息
// 这个功能是使 UPS 能回答额定值信息。每个信息段的之间有一个空格符。
// 输入：F<CR>
// 输出：#MMM.M QQQ SS.SS RR.R<CR>
//
//	#220.0 007 24.00 50.0
//
// 信息段格式定义如下:
// 额定电压:MMM.M
// 额定电流:QQQ
// 电池电压:SS.SS 或 SSS.S
// 额定频率:RR.R
type RatingInfo struct {
	VoltageRating   float32
	CurrentRating   int
	BatteryVoltage  float32
	FrequencyRating float32
}

// -- 三进三出 --

// G1<cr>
// UPS : !240 094 0123 025.0 +35.0 50.1 52.0 50.0<cr>
type ExtraQueryResult struct {
	BatteryVoltage       int     // 电池电压 SSS
	BatteryCapacity      int     // 电池容量 PPP
	BatteryTimeRemaining int     // 剩余时间 NNNN
	BatteryCurrent       float32 // 电池电流 RRR.R
	Temperature          float32 // 温度 +TT.T
	IPFreq               float32 // 输入频率 FF.FF
	BypassFreq           float32 // Bypass 频率 EE.EE
	OPFreq               float32 // 输出频率 QQ.Q
}

// G2<cr>
// UPS : !00000010 00000100 00000000<cr>
type ExtraQueryError struct {
	// A 组
	Rectifier            bool // a6 整流器故障
	BatteryLowProtection bool // a5 电池低压保护
	BatteryLow           bool // a4 电池低压
	TPInOneOut           bool // a3 1 :  三相输入–单相输出  0 :  三相输入–三相输出
	BatterySupply        bool // a2 1 :  后备供电中         0 :  交流输入正常
	BatteryEqualization  bool // a1 1 :  对电池进行均充状态  0 :  对电池进行浮充状态
	RectifierRunning     bool // a0 1 :  整流器运行中

	// B 组
	BypassFreqError bool // b4 Bypass 频率异常
	ManualBypass    bool // b3 1 :  手动旁路闭合  0 :  手动旁路断开
	BypassNomal     bool // b2 1 :  旁路交流电正常  0 :  旁路交流电异常
	StaticBypass    bool // b1 1 :  静态旁路开关处于逆变端  0 :  静态旁路开关处于旁路端
	InverterRunning bool // b0 1 :  逆变器运行中  0 :  逆变器停止

	// C 组
	EmergencyStop         bool // c6 紧急停机
	BatteryInputHigh      bool // c5 电池输入高压
	ManualBypassStop      bool // c4 手动旁路闭合停机
	OverloadStop          bool // c3 过载停机
	InverterOutputVoltage bool // c2 逆变器输出电压异常
	OverTemperature       bool // c1 过温
	OutputShortCircuit    bool // c0 输出短路
}

// G3<cr>
// UPS : !222.0/222.0/222.0 221.0/221.0/221.0 220.0/220.0/220.0 014.0/015.0/014.0<cr>
type TPInfo struct {
	InputR float32 // 输入电压 R 相
	InputS float32 // 输入电压 S 相
	InputT float32 // 输入电压 T 相

	BypassR float32 // 旁路电压 R 相
	BypassS float32 // 旁路电压 S 相
	BypassT float32 // 旁路电压 T 相

	OutputR float32 // 输出电压 R 相
	OutputS float32 // 输出电压 S 相
	OutputT float32 // 输出电压 T 相

	RPercent float32 // R 相负载百分比
	SPercent float32 // S 相负载百分比
	TPercent float32 // T 相负载百分比
}

// GF<cr>
// UPS : !220V/380V^3P4W 050 220V/380V^3P4W 050 220V/3P3W^^^^^ 050 396 150KVA^^^^<cr>
type TPRating struct {
	RectifierInfo string // 整流器额定信息
	RectifierFreq int    // 整流器频率

	BypassInfo string // 旁路额定信息
	BypassFreq int    // 旁路频率

	OuputInfo string // 输出额定信息
	OuputFreq int    // 输出频率

	BatteryVoltage int // 电池额定电压

	PowerRating string // 功率额定值
}

func parseFloat(s string) float32 {
	if f, err := strconv.ParseFloat(s, 32); err == nil {
		return float32(f)
	}
	return 0
}

func ProtoParse(data string) any {
	if len(data) == 0 {
		return nil
	}
	startByte := data[0]
	data = data[1:]
	switch startByte {
	case QueryByteChar:
		return ParseQueryResult(data)
	case RatingByteChar:
		return ParseRatingInfo(data)
	case ExtraQueryByteChar:
		return ParseExtra(data)
	default:
		for i, c := range data {
			if c == QueryByteChar || c == RatingByteChar || c == ExtraQueryByteChar {
				return ProtoParse(data[i:])
			}
		}
		return nil
	}
}

func ParseQueryResult(data string) QueryResult {
	var result QueryResult
	split := strings.Split(data, " ")
	if len(split) != 8 {
		return result
	}
	result.IPVoltage = parseFloat(split[0])
	result.IPFaultVoltage = parseFloat(split[1])
	result.OPVoltage = parseFloat(split[2])
	result.OPCurrentPercent, _ = strconv.Atoi(split[3])
	result.IPFreq = parseFloat(split[4])
	result.BatteryVoltage = parseFloat(split[5])
	result.Temperature = parseFloat(split[6])
	for i, c := range split[7] {
		switch i {
		case 0:
			result.Status.UtilityFail = c == '1'
		case 1:
			result.Status.BatteryLow = c == '1'
		case 2:
			result.Status.BypassBoostActive = c == '1'
		case 3:
			result.Status.UPSFailed = c == '1'
		case 4:
			result.Status.UPSType = c == '1'
		case 5:
			result.Status.TestActive = c == '1'
		case 6:
			result.Status.ShutdownActive = c == '1'
		case 7:
			result.Status.BuzzerActive = c == '1'
		}
	}
	return result
}

func ParseRatingInfo(data string) RatingInfo {
	var result RatingInfo
	split := strings.Split(data, " ")
	if len(split) != 4 {
		return result
	}
	result.VoltageRating = parseFloat(split[0])
	result.CurrentRating, _ = strconv.Atoi(split[1])
	result.BatteryVoltage = parseFloat(split[2])
	result.FrequencyRating = parseFloat(split[3])
	return result
}

func ParseExtra(data string) any {
	split := strings.Split(data, " ")
	switch len(split) {
	case 8:
		return ParseExtraQueryResult(split)
	case 3:
		return ParseExtraQueryError(split)
	case 4:
		return ParseTPInfo(split)
	case 5:
		return ParseTPRating(split)
	default:
		return nil
	}
}

func ParseExtraQueryResult(split []string) ExtraQueryResult {
	var result ExtraQueryResult
	result.BatteryVoltage, _ = strconv.Atoi(split[0])
	result.BatteryCapacity, _ = strconv.Atoi(split[1])
	result.BatteryTimeRemaining, _ = strconv.Atoi(split[2])
	result.BatteryCurrent = parseFloat(split[3])
	result.Temperature = parseFloat(split[4])
	result.IPFreq = parseFloat(split[5])
	result.BypassFreq = parseFloat(split[6])
	result.OPFreq = parseFloat(split[7])
	return result
}

func ParseExtraQueryError(split []string) ExtraQueryError {
	var result ExtraQueryError

	// A 组
	for i, c := range split[0] {
		switch i {
		case 0:
			result.Rectifier = c == '1'
		case 1:
			result.BatteryLowProtection = c == '1'
		case 2:
			result.BatteryLow = c == '1'
		case 3:
			result.TPInOneOut = c == '1'
		case 4:
			result.BatterySupply = c == '1'
		case 5:
			result.BatteryEqualization = c == '1'
		case 6:
			result.RectifierRunning = c == '1'
		}
	}

	// B 组
	for i, c := range split[1] {
		switch i {
		case 0:
			result.BypassFreqError = c == '1'
		case 1:
			result.ManualBypass = c == '1'
		case 2:
			result.BypassNomal = c == '1'
		case 3:
			result.StaticBypass = c == '1'
		case 4:
			result.InverterRunning = c == '1'
		}
	}

	// C 组
	for i, c := range split[2] {
		switch i {
		case 0:
			result.EmergencyStop = c == '1'
		case 1:
			result.BatteryInputHigh = c == '1'
		case 2:
			result.ManualBypassStop = c == '1'
		case 3:
			result.OverloadStop = c == '1'
		case 4:
			result.InverterOutputVoltage = c == '1'
		case 5:
			result.OverTemperature = c == '1'
		case 6:
			result.OutputShortCircuit = c == '1'
		}
	}

	return result
}

func ParseTPInfo(split []string) TPInfo {
	var result TPInfo

	info := strings.Split(split[0], "/")
	result.InputR = parseFloat(info[0])
	result.InputS = parseFloat(info[1])
	result.InputT = parseFloat(info[2])

	info = strings.Split(split[1], "/")
	result.BypassR = parseFloat(info[0])
	result.BypassS = parseFloat(info[1])
	result.BypassT = parseFloat(info[2])

	info = strings.Split(split[2], "/")
	result.OutputR = parseFloat(info[0])
	result.OutputS = parseFloat(info[1])
	result.OutputT = parseFloat(info[2])

	return result
}

func ParseTPRating(split []string) TPRating {
	var result TPRating

	result.RectifierInfo = strings.TrimSpace(strings.Replace(split[0], "^", " ", -1))
	result.RectifierFreq, _ = strconv.Atoi(split[1])

	result.BypassInfo = strings.TrimSpace(strings.Replace(split[2], "^", " ", -1))
	result.BypassFreq, _ = strconv.Atoi(split[3])

	result.OuputInfo = strings.TrimSpace(strings.Replace(split[4], "^", " ", -1))
	result.OuputFreq, _ = strconv.Atoi(split[5])

	result.BatteryVoltage, _ = strconv.Atoi(split[6])

	result.PowerRating = strings.TrimSpace(strings.Replace(split[7], "^", " ", -1))

	return result
}
