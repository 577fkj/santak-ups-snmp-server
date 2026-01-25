package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/hallidave/mibtool/smi"
	"github.com/slayercat/GoSNMPServer"
)

type TimesTamp uint32

type AlarmEntry struct {
	Index int
	Descr string
	Time  TimesTamp
}

type SNMPDataIdent struct { // 基本信息
	Manufacturer    string `snmp:"upsIdentManufacturer:upsIdentGroupManufacturer"`                 // 制造商
	Model           string `snmp:"upsIdentModel:upsIdentGroupModel"`                               // 型号
	SoftwareVersion string `snmp:"upsIdentUPSSoftwareVersion:upsIdentGroupUPSFirmwareVersion"`     // UPS软件版本
	AgentVersion    string `snmp:"upsIdentAgentSoftwareVersion:upsIdentGroupAgentSoftwareVersion"` // Agent软件版本
	Name            string `snmp:"upsIdentName,w:upsIdentGroupName,w"`                             // 名称
	AttachedDevices string `snmp:"upsIdentAttachedDevices,w:upsIdentGroupAttachedDevices,w"`       // 连接设备

	// USHA-MIB
	SerialNumber string `snmp:"upsIdentGroupUpsSerialNumber"` // 序列号
}

type SNMPDataBattery struct { // 电池信息
	Status      int `snmp:"upsBatteryStatus:upsBatteryGroupStatus"`                                // 状态 1: unknown, 2: batteryNormal, 3: batteryLow, 4: batteryDepleted
	Seconds     int `snmp:"upsSecondsOnBattery:upsBatteryGroupSecondsOnBattery"`                   // 已经在电池上运行的时间
	Minutes     int `snmp:"upsEstimatedMinutesRemaining:upsBatteryGroupEstimatedMinutesRemaining"` // 估计剩余时间(分钟)
	Charge      int `snmp:"upsEstimatedChargeRemaining:upsBatteryGroupEstimatedChargeRemaining"`   // 估计剩余电量(%) 0-100
	Voltage     int `snmp:"upsBatteryVoltage:upsBatteryGroupVoltage"`                              // 电池电压
	Current     int `snmp:"upsBatteryCurrent"`                                                     // 电池电流
	Temp        int `snmp:"upsBatteryTemperature"`                                                 // 电池温度
	Temperature int `snmp:"upsBatteryGroupTemperature"`                                            // 电池温度

	// USHA-MIB
	Mandatory int `snmp:"upsBatteryGroupMandatory"` // 是否为强制电池?
}

type SNMPDataInput struct { // 输入信息
	LineBads int `snmp:"upsInputLineBads:upsInputGroupLineBads"` // 输入线路故障数
	NumLines int `snmp:"upsInputNumLines:upsInputGroupNumLines"` // 输入线路数

	// ------------------------------------------------
	// Table: upsInputTable
	// ------------------------------------------------
	// upsInputLineIndex  int  输入线路索引
	// upsInputFrequency  int  输入频率
	// upsInputVoltage    int  输入电压
	// upsInputCurrent    int  输入电流
	// upsInputTruePower  int  输入功率
}

type SNMPDataOutput struct { // 输出信息
	Source   int `snmp:"upsOutputSource:upsOutputGroupSource"`       // 输出源 1: other, 2: none, 3: normal, 4: bypass, 5: battery, 6: booster, 7: reducer
	Freq     int `snmp:"upsOutputFrequency:upsOutputGroupFrequency"` // 输出频率
	NumLines int `snmp:"upsOutputNumLines:upsOutputGroupNumLines"`   // 输出线路数

	// ------------------------------------------------
	// Table: upsOutputTable
	// ------------------------------------------------
	// upsOutputLineIndex   int
	// upsOutputVoltage     int
	// upsOutputCurrent     int
	// upsOutputPower       int
	// upsOutputPercentLoad int
}

type SNMPDataBypass struct {
	Freq     int `snmp:"upsBypassFrequency:upsBypassGroupFrequency"` // 旁路频率
	NumLines int `snmp:"upsBypassNumLines:upsBypassGroupNumLines"`   // 旁路线路数

	// ------------------------------------------------
	// Table: upsBypassTable
	// ------------------------------------------------
	// upsBypassLineIndex   int
	// upsBypassVoltage     int
	// upsBypassCurrent     int
	// upsBypassPower       int
}

type SNMPDataAlarm struct {
	Present int `snmp:"upsAlarmsPresent"`

	// ------------------------------------------------
	// Table: upsAlarmTable
	// ------------------------------------------------
	// upsAlarmIndex   int
	// upsAlarmDescr   string
	// upsAlarmTime    TimeStamp
}

type SNMPDataTest struct {
	Id             string    `snmp:"upsTestId,w"`                                      // 当前测试ID
	SpinLock       int       `snmp:"upsTestSpinLock,w"`                                // 测试锁，自旋锁
	ResultsSummary int       `snmp:"upsTestResultsSummary:upsTestBatteryTestResult"`   // 测试状态 1: done, 2: done Warn, 3: done Error, 4: aborted, 5: in progress, 6: noRun
	ResultsDetail  string    `snmp:"upsTestResultsDetail"`                             // 测试结果
	StartTime      TimesTamp `snmp:"upsTestStartTime:upsTestBatteryTestStartTime"`     // 测试开始时间
	ElapsedTime    TimesTamp `snmp:"upsTestElapsedTime:upsTestBatteryTestElapsedTime"` // 测试持续时间

	// USHA-MIB
	BatteryTestSettingTime int `snmp:"upsTestBatteryTestSettingTime,w"` // 电池测试设置时间
	BatteryTest            int `snmp:"upsBatteryTest,w"`                // 电池测试类型 1: none, 2: battTest10sec, 3: battTestUntilLow, 4: battTestWithTime, 5: battTestCancelTest, 6: battTestClearInfo

	// upsBatteryTestScheduleTable // 电池测试计划表
	// 		upsBatteryTestScheduleEntry // 电池测试计划条目
	// 				upsBatteryTestScheduleIndex // 索引
	// 				upsBatteryTestScheduleDay // 星期几
	// 				upsBatteryTestScheduleTime // 时间
	// 				upsBatteryTestScheduleType // 测试类型
	// 				upsBatteryTestScheduleTestWithTime // 测试时间
	// 				upsBatteryTestScheduleSpecialDay // 特殊日期

	// --
	// Id
	// --
	// upsTestNoTestsInitiated        // 未测试
	// upsTestAbortTestInProgress     // 测试中止
	// upsTestGeneralSystemsTest      // 一般系统测试
	// upsTestQuickBatteryTest        // 快速电池测试
	// upsTestDeepBatteryCalibration  // 深度电池校准
}

type SNMPDataControl struct {
	ShutdownType   int `snmp:"upsShutdownType,w"`                                    // 1: output, 2: system
	ShutdownAfter  int `snmp:"upsShutdownAfterDelay,w:upsControlUpsShutdownDelay,w"` // 关机延迟时间
	StartupAfter   int `snmp:"upsStartupAfterDelay,w"`                               // 启动延迟时间
	RebootDuration int `snmp:"upsRebootWithDuration,w:upsControlUpsSleepTime,w"`     // 重启持续时间
	AutoRestart    int `snmp:"upsAutoRestart,w"`                                     // 1: on, 2: off

	// USHA-MIB
	OnOffControl int `snmp:"upsControlUpsOnOffControl,w"` // 开关控制 1: turnUpsOff, 2: putUpsToSleep, 3: turnOnUpsOrCancelShutdown, 4: none

	// upsControlShutdownParametersTable // 关机参数表
	// 		upsControlShutdownParametersEntry // 关机参数条目
	// 				upsControlEvent // 事件类型
	// 				upsControlEventStatus // 事件状态
	// 				upsControlDelay // 延迟时间
	// 				upsControlFirstWarning // 首次警告时间
	// 				upsControlWarningInterval // 警告间隔时间

	// upsControlWeeklyScheduleTable // 每周计划表
	// 		upsControlWeeklyScheduleEntry // 每周计划条目
	// 				upsControlWeeklyIndex // 索引
	// 				upsControlWeeklyShutdownDay // 关机星期几
	// 				upsControlWeeklyShutdownTime // 关机时间
	// 				upsControlWeeklyRestartDay // 重启星期几
	// 				upsControlWeeklyRestartTime // 重启时间

	// upsControlSpecialScheduleTable // 特殊计划表
	// 		upsControlSpecialScheduleEntry // 特殊计划条目
	// 				upsControlSpecialIndex // 索引
	// 				upsControlSpecialShutdownDay // 特殊关机日期
	// 				upsControlSpecialShutdownTime // 特殊关机时间
	// 				upsControlSpecialRestartDay // 特殊重启日期
	// 				upsControlSpecialRestartTime // 特殊重启时间
}

type SNMPDataConfig struct {
	InputVoltage             int `snmp:"upsConfigInputVoltage,w:upsConfigGroupInputVoltage"`
	InputFreq                int `snmp:"upsConfigInputFreq,w:upsConfigGroupInputFreq"`
	OutputVoltage            int `snmp:"upsConfigOutputVoltage,w:upsConfigGroupOutputVoltage"`
	OutputFreq               int `snmp:"upsConfigOutputFreq,w:upsConfigGroupOutputFreq"`
	OutputVA                 int `snmp:"upsConfigOutputVA:upsConfigGroupOutputVA"`
	OutputPower              int `snmp:"upsConfigOutputPower:upsConfigGroupOutputPower"`
	LowBatteryTime           int `snmp:"upsConfigLowBattTime,w"`
	AudibleStatus            int `snmp:"upsConfigAudibleStatus,w"` // 蜂鸣器 1: disable, 2: enable, 3: mute
	LowVoltageTransferPoint  int `snmp:"upsConfigLowVoltageTransferPoint,w"`
	HighVoltageTransferPoint int `snmp:"upsConfigHighVoltageTransferPoint,w"`

	// USHA-MIB
	OverTemperatureSetPoint int `snmp:"upsConfigGroupOverTemperatureSetPoint,w"` // 过温设定点
	OverLoadSetPoint        int `snmp:"upsConfigGroupOverLoadSetPoint,w"`        // 过载设定点
}

// USHA-MIB 注册关机客户端信息
type SNMPDataUPSClients struct {
	ConnectedNum int `snmp:"upsClientConnectedNum"` // 连接的客户端数量

	// upsDevicesTable // 连接设备表
	// 	upsDevicesEntry // 连接设备条目
	// 		indexOfDevice // 设备索引
	// 		addrOfDevice // 设备地址
	// 		nameOfDevice // 设备名称
	// 		timeOfConnection // 连接时间
	// 		timeOfConnectionTime // 连接时间戳
	// 		timeOfConnectionTimeout // 连接超时
}

// Agent 配置相关
type SNMPDataAgentConfig struct {
	IPAddress              string `snmp:"agentConfigIpaddress,w"`              // 设备IP地址
	Gateway                string `snmp:"agentConfigGateway,w"`                // 网关地址
	SubnetMask             string `snmp:"agentConfigSubnetMask,w"`             // 子网掩码
	Date                   string `snmp:"agentConfigDate,w"`                   // 当前日期（格式，例如 YYYY-MM-DD）
	Time                   string `snmp:"agentConfigTime,w"`                   // 当前时间（格式，例如 HH:MM:SS）
	PrimaryTimeServer      string `snmp:"agentConfigPrimaryTimeServer,w"`      // 主时间服务器地址
	SecondaryTimeServer    string `snmp:"agentConfigSecondaryTimeServer,w"`    // 备用时间服务器地址
	HistoryLogFrequency    int    `snmp:"agentConfigHistoryLogFrequency,w"`    // 历史日志记录频率（单位可为分钟或条数，见MIB定义）
	ExtHistoryLogFrequency int    `snmp:"agentConfigExtHistoryLogFrequency,w"` // 扩展历史日志记录频率
	PollRate               int    `snmp:"agentConfigPollRate,w"`               // 轮询速率（秒）
	BaudRate               int    `snmp:"agentConfigBaudRate"`                 // 串口波特率
	DhcpStatue             int    `snmp:"agentConfigDhcpStatue,w"`             // DHCP 状态（例如 1: enabled, 2: disabled）
	TelnetStatue           int    `snmp:"agentConfigTelnetStatue,w"`           // Telnet 服务状态（启用/禁用）
	TftpStatue             int    `snmp:"agentConfigTftpStatue,w"`             // TFTP 服务状态（启用/禁用）
	ResetToDefault         int    `snmp:"agentConfigResetToDefault,w"`         // 重置为默认配置（触发动作）
	Restart                int    `snmp:"agentConfigRestart,w"`                // 重启设备（触发动作）
	ClearAgentLog          int    `snmp:"agentConfigClearAgentLog,w"`          // 清除 Agent 日志（触发动作）
	ClearEventLog          int    `snmp:"agentConfigClearEventLog,w"`          // 清除事件日志（触发动作）
	ClearExtHistoryLog     int    `snmp:"agentConfigClearExtHistoryLog,w"`     // 清除扩展历史日志（触发动作）
	ClearHistoryLog        int    `snmp:"agentConfigClearHistoryLog,w"`        // 清除历史日志（触发动作）
	TrapRetryCount         int    `snmp:"agentConfigTrapRetryCount,w"`         // Trap 重试次数
	TrapRetryTime          int    `snmp:"agentConfigTrapRetryTime,w"`          // Trap 重试间隔时间（秒）
	TrapAckSignature       int    `snmp:"agentConfigTrapAckSignature,w"`       // Trap 确认签名开关（例如 1: on, 2: off）
	MibVersion             string `snmp:"agentConfigMibVersion"`               // MIB 版本字符串
	DefaultLanguage        string `snmp:"agentConfigDefaultLanguage,w"`        // 默认语言

	// agentConfigTrapsReceiversTable // Trap 接收者表
	//  agentConfigTrapsReceiversEntry
	//      agentConfigTrapsReceiversIndex // 索引
	//      agentConfigTrapsReceiversAddress // 接收者地址
	//      agentConfigTrapsReceiversCommunity // 社区字符串
	//      agentConfigTrapsReceiversVersion // SNMP 版本
	//
	// agentConfigAccessControlTable // 访问控制表
	//  agentConfigAccessControlEntry
	//      agentConfigAccessControlIndex // 索引
	//      agentConfigAccessControlAddress // 允许/拒绝的地址
	//      agentConfigAccessControlMask // 地址掩码
	//      agentConfigAccessControlPermission // 权限（读/写/全部）
}

// EMD 状态（环境监测设备）
type SNMPDataEmdStatus struct {
	EmType      int `snmp:"emdSatatusEmdType"`     // EMD 类型标识
	Temperature int `snmp:"emdSatatusTemperature"` // 当前温度（单位取决于设备，通常为 0.1°C 或 °C）
	Humidity    int `snmp:"emdSatatusHumidity"`    // 当前湿度（百分比）
	Alarm1      int `snmp:"emdSatatusAlarm1"`      // 报警1 状态（例如 1: normal, 2: alarm）
	Alarm2      int `snmp:"emdSatatusAlarm2"`      // 报警2 状态
}

// USHA-MIB EMD 配置
type SNMPDataEmdConfig struct {
	UsahEmdConfigEmdConfig int    `snmp:"usahEmdConfigEmdConfig,w"` // USHA-MIB: EMD 配置索引或启用标志
	EmdName                string `snmp:"emdConfigEmdName,w"`       // EMD 名称

	// 温度相关
	TempName         string `snmp:"emdConfigTempName,w"`         // 温度传感器名称
	TempHighSetPoint int    `snmp:"emdConfigTempHighSetPoint,w"` // 温度高阈值（报警上限）
	TempHighStatus   int    `snmp:"emdConfigTempHighStatus,w"`   // 温度高阈值报警状态
	TempLowSetPoint  int    `snmp:"emdConfigTempLowSetPoint,w"`  // 温度低阈值（报警下限）
	TempLowStatus    int    `snmp:"emdConfigTempLowStatus,w"`    // 温度低阈值报警状态
	TempOffset       int    `snmp:"emdConfigTempOffset,w"`       // 温度偏移校正值

	// 湿度相关
	HumidityName         string `snmp:"emdConfigHumidityName,w"`         // 湿度传感器名称
	HumidityHighSetPoint int    `snmp:"emdConfigHumidityHighSetPoint,w"` // 湿度高阈值
	HumidityHighStatus   int    `snmp:"emdConfigHumidityHighStatus,w"`   // 湿度高阈值报警状态
	HumidityLowSetPoint  int    `snmp:"emdConfigHumidityLowSetPoint,w"`  // 湿度低阈值
	HumidityLowStatus    int    `snmp:"emdConfigHumidityLowStatus,w"`    // 湿度低阈值报警状态
	HumidityOffset       int    `snmp:"emdConfigHumidityOffset,w"`       // 湿度偏移校正值

	// 报警通道
	Alarm1Name string `snmp:"emdConfigAlarm1Name,w"` // 报警1 名称
	Alarm1Type int    `snmp:"emdConfigAlarm1Type,w"` // 报警1 类型

	Alarm2Name string `snmp:"emdConfigAlarm2Name,w"` // 报警2 名称
	Alarm2Type int    `snmp:"emdConfigAlarm2Type,w"` // 报警2 类型
}

type SNMPData struct {
	Ident      *SNMPDataIdent      `snmp:"upsIdent"`
	Battery    *SNMPDataBattery    `snmp:"upsBattery"`
	Input      *SNMPDataInput      `snmp:"upsInput"`
	Output     *SNMPDataOutput     `snmp:"upsOutput"`
	Bypass     *SNMPDataBypass     `snmp:"upsBypass"`
	Alarm      *SNMPDataAlarm      `snmp:"upsAlarm"`
	Test       *SNMPDataTest       `snmp:"upsTest"`
	Control    *SNMPDataControl    `snmp:"upsControl"`
	Config     *SNMPDataConfig     `snmp:"upsConfig"`
	UPSClients *SNMPDataUPSClients `snmp:"upsClients"`

	AgentConfig *SNMPDataAgentConfig `snmp:"agentConfig"` // Agent 配置
	EmdStatus   *SNMPDataEmdStatus   `snmp:"emdStatus"`   // EMD 状态
	EmdConfig   *SNMPDataEmdConfig   `snmp:"emdConfig"`   // EMD 配置

	UserData any
}

// SNMPFieldInfo 存储字段信息
type SNMPFieldInfo struct {
	FieldName string
	Id        string
	FieldType string
	Writable  bool
	SNMPType  string
}

type SNMP struct {
	Device  Device
	TtySend func(cmd string)

	Data *SNMPData

	Config *SNMPConfig

	Trap             []*gosnmp.GoSNMP
	TrapAgentAddress string

	Server  *GoSNMPServer.SNMPServer
	Master  *GoSNMPServer.MasterAgent
	Public  *GoSNMPServer.SubAgent
	Private *GoSNMPServer.SubAgent
	Mib     *smi.MIB
}

type SNMPAuth struct {
	Username string
	AuthKey  string
	PrivKey  string

	AuthProto gosnmp.SnmpV3AuthProtocol
	PrivProto gosnmp.SnmpV3PrivProtocol
}

type SNMPConfig struct {
	Address string
	Port    int

	Logger GoSNMPServer.ILogger

	PublicName  string
	PrivateName string

	Auth []SNMPAuth

	SetCallback func(snmp *SNMP, name string, value interface{}) error
}

func getTypeName(t reflect.Type) string {
	fullName := t.String()
	lastDot := strings.LastIndex(fullName, ".")
	if lastDot != -1 {
		return fullName[lastDot+1:] // 切片获取类型名
	}
	return fullName // 如果没有点，返回完整的类型名
}

func getFieldInfoFromType(t reflect.Type) []SNMPFieldInfo {
	var fieldInfos []SNMPFieldInfo

	// 如果传入的类型不是结构体，直接返回空
	if t.Kind() != reflect.Struct {
		return fieldInfos
	}

	// 遍历结构体的字段
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		tag := field.Tag.Get("snmp")
		if tag != "" {
			ids := strings.Split(tag, ":")
			for _, id := range ids {
				// 解析snmp标签
				parts := strings.Split(id, ",")
				m_id := parts[0]  // 第一个部分为OID
				writable := false // 默认不可写
				snmpType := ""    // 默认无类型

				// 检查额外选项
				for _, part := range parts[1:] {
					if part == "w" {
						writable = true
					} else {
						snmpType = part
					}
				}

				fieldInfos = append(fieldInfos, SNMPFieldInfo{
					FieldName: field.Name,
					Id:        m_id,
					FieldType: getTypeName(field.Type),
					Writable:  writable,
					SNMPType:  snmpType,
				})
			}
		}

		// 处理嵌套结构体或指针类型
		if field.Type.Kind() == reflect.Struct {
			nestedFieldInfos := getFieldInfoFromType(field.Type)
			fieldInfos = append(fieldInfos, nestedFieldInfos...)
		} else if field.Type.Kind() == reflect.Ptr {
			// 如果是指针类型，处理指针指向的类型
			nestedFieldInfos := getFieldInfoFromType(field.Type.Elem())
			fieldInfos = append(fieldInfos, nestedFieldInfos...)
		}
	}

	return fieldInfos
}

func snmp_server(config SNMPConfig, server_enable SNMPData, data *SNMPData) *SNMP {
	snmp := &SNMP{
		Data:   data,
		Config: &config,
	}

	// 读写共同体
	useRW := config.PublicName == config.PrivateName

	master := GoSNMPServer.MasterAgent{
		SecurityConfig: GoSNMPServer.SecurityConfig{
			AuthoritativeEngineBoots: 1,
			Users:                    []gosnmp.UsmSecurityParameters{},
		},
	}

	if config.Auth != nil {
		master.SecurityConfig.SnmpV3Only = true
		for _, auth := range config.Auth {
			master.SecurityConfig.Users = append(master.SecurityConfig.Users, gosnmp.UsmSecurityParameters{
				UserName:                 auth.Username,
				AuthenticationProtocol:   auth.AuthProto,
				PrivacyProtocol:          auth.PrivProto,
				AuthenticationPassphrase: auth.AuthKey,
				PrivacyPassphrase:        auth.PrivKey,
			})
		}
	}

	public := GoSNMPServer.SubAgent{
		CommunityIDs: []string{config.PublicName},
	}

	var private GoSNMPServer.SubAgent
	if !useRW {
		private = GoSNMPServer.SubAgent{
			CommunityIDs: []string{config.PrivateName},
		}
		master.SubAgents = []*GoSNMPServer.SubAgent{&public, &private}
	} else {
		master.SubAgents = []*GoSNMPServer.SubAgent{&public}
	}

	if config.Logger != nil {
		master.Logger = config.Logger
	} else {
		master.Logger = GoSNMPServer.NewDefaultLogger()
	}

	path, err := os.Getwd()
	if err != nil {
		master.Logger.Fatalf("Get path faild: %s", err.Error())
		return nil
	}

	mib := smi.NewMIB(filepath.Join(path, "mibs"))
	err = mib.LoadModules("UPS-MIB")
	if err != nil {
		master.Logger.Fatalf("Get MIB faild: %s", err.Error())
		return nil
	}

	err = mib.LoadModules("USHA-MIB")
	if err != nil {
		master.Logger.Fatalf("Get MIB faild: %s", err.Error())
		return nil
	}

	snmp.Mib = mib

	ids := getFieldInfoFromType(reflect.TypeOf(SNMPData{}))

	// helper: 在 root 中查找名为 name 的字段（支持嵌套指针结构）
	findField := func(root any, name string) (reflect.Value, bool) {
		rv := reflect.ValueOf(root)
		if !rv.IsValid() {
			return reflect.Value{}, false
		}
		if rv.Kind() == reflect.Ptr {
			if rv.IsNil() {
				return reflect.Value{}, false
			}
			rv = rv.Elem()
		}
		if rv.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}

		// 直接查找
		if f := rv.FieldByName(name); f.IsValid() {
			return f, true
		}

		// 在顶级字段中查找嵌套结构的字段
		for i := 0; i < rv.NumField(); i++ {
			f := rv.Field(i)
			if f.Kind() == reflect.Ptr {
				if f.IsNil() {
					continue
				}
				e := f.Elem()
				if e.Kind() == reflect.Struct {
					if ff := e.FieldByName(name); ff.IsValid() {
						return ff, true
					}
				}
			} else if f.Kind() == reflect.Struct {
				if ff := f.FieldByName(name); ff.IsValid() {
					return ff, true
				}
			}
		}
		return reflect.Value{}, false
	}

	for _, id := range ids {
		m_id := id.Id
		name := id.FieldName
		type_name := id.FieldType

		// 在实际数据对象中查找字段
		field, ok := findField(data, name)
		if !ok {
			master.Logger.Debugf("Skip unknown data field: %s", name)
			continue
		}

		// 在 server_enable 中查找对应的使能字段
		enableField, okEnable := findField(server_enable, name)

		oid, err := mib.OID(m_id)
		if err != nil {
			panic(err)
		}

		var tp gosnmp.Asn1BER
		switch type_name {
		case "string":
			tp = gosnmp.OctetString
		case "int":
			tp = gosnmp.Integer
		case "TimesTamp":
			tp = gosnmp.TimeTicks
		default:
			// 非基础类型（结构体），不直接创建 OID，继续
			continue
		}

		// 如果找不到 enable 字段或其值为零，则跳过
		if !okEnable {
			master.Logger.Debugf("Skip service [%s](%s) no enable info", name, m_id)
			continue
		}
		// treat pointer enable fields
		if enableField.Kind() == reflect.Ptr {
			if enableField.IsNil() || enableField.Elem().IsZero() {
				master.Logger.Debugf("Skip service [%s](%s) %s", name, m_id, oid.String())
				continue
			}
		} else {
			if enableField.IsZero() {
				master.Logger.Debugf("Skip service [%s](%s) %s", name, m_id, oid.String())
				continue
			}
		}

		oid_str := fmt.Sprintf(".%s.0", oid.String())

		master.Logger.Infof("Add service [%s](%s) %s", name, m_id, oid_str)

		var onSet func(value interface{}) error
		if id.Writable {
			master.Logger.Infof("Add service [%s](%s) %s is writable", name, m_id, oid_str)
			onSet = func(value interface{}) error {
				Logger.Debugf("Set: %s", name)
				if !field.IsValid() {
					return fmt.Errorf("field not found")
				}
				field.Set(reflect.ValueOf(value))
				if config.SetCallback != nil {
					return config.SetCallback(snmp, name, value)
				}
				return nil
			}
			if !useRW {
				private.OIDs = append(private.OIDs, &GoSNMPServer.PDUValueControlItem{
					OID:   oid_str,
					Type:  tp,
					OnSet: onSet,
				})
				onSet = nil
			}
		}
		// capture field, name locally for closure
		fCopy := field
		nameCopy := name
		public.OIDs = append(public.OIDs, &GoSNMPServer.PDUValueControlItem{
			OID:  oid_str,
			Type: tp,
			OnGet: func() (interface{}, error) {
				master.Logger.Debugf("Get: %s", nameCopy)
				if !fCopy.IsValid() {
					return nil, fmt.Errorf("field not found")
				}
				value := fCopy.Interface()
				master.Logger.Debugf("Get data: %s", value)
				switch v := value.(type) {
				case TimesTamp:
					value = uint32(v)
				default:
					value = v
				}
				return value, nil
			},
			OnSet: onSet,
		})
	}

	listen := fmt.Sprintf("%s:%d", config.Address, config.Port)

	// 创建并启动服务器
	server := GoSNMPServer.NewSNMPServer(master)
	err = server.ListenUDP("udp", listen)
	if err != nil {
		master.Logger.Fatalf("Error in listen: %+v", err)
	}

	snmp.Server = server
	snmp.Master = &master
	snmp.Public = &public
	if !useRW {
		snmp.Private = &private
	}

	return snmp
}

type TrapConfig struct {
	Host      string
	Port      uint16
	Community string

	Version gosnmp.SnmpVersion

	Auth *SNMPAuth
}

type TrapData struct {
	OID  string
	Data []TrapDataItem
}

type TrapDataItem struct {
	OID   string
	Type  gosnmp.Asn1BER
	Value interface{}
}

func (s *SNMP) AddTrap(config TrapConfig) error {
	g := &gosnmp.GoSNMP{
		Target:    config.Host,
		Port:      config.Port,
		Version:   config.Version,
		Community: config.Community,
		Timeout:   time.Duration(2) * time.Second,
		Logger:    gosnmp.NewLogger(SNMPLogger),
	}

	if config.Auth != nil {
		g.Version = gosnmp.Version3
		g.MsgFlags = gosnmp.AuthPriv
		g.SecurityModel = gosnmp.UserSecurityModel
		g.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 config.Auth.Username,
			AuthenticationProtocol:   config.Auth.AuthProto,
			PrivacyProtocol:          config.Auth.PrivProto,
			AuthenticationPassphrase: config.Auth.AuthKey,
			PrivacyPassphrase:        config.Auth.PrivKey,
		}
	}

	// 初始化连接
	err := g.Connect()
	if err != nil {
		SNMPLogger.Errorf("Connect to SNMP trap server faild: %s", err.Error())
		return err
	}

	s.Trap = append(s.Trap, g)

	return nil
}

func (s *SNMP) SendTrap(data TrapData) error {
	if len(s.Trap) == 0 {
		return nil
	}

	enterprise, specific, err := ExtractEnterpriseIDAndSpecificTrap(s.GetOID(data.OID, -1))
	if err != nil {
		panic(err.Error())
	}

	trap := gosnmp.SnmpTrap{
		Variables: []gosnmp.SnmpPDU{
			{
				Name:  s.GetOID(data.OID, -1),
				Type:  gosnmp.ObjectIdentifier,
				Value: s.GetOID(data.OID, -1),
			},
		},
		Enterprise:   enterprise,
		AgentAddress: s.TrapAgentAddress,
		GenericTrap:  6,
		SpecificTrap: specific,
		Timestamp:    uint(getRunningTimeInSeconds() * 100),
	}
	for _, v := range data.Data {
		trap.Variables = append(trap.Variables, gosnmp.SnmpPDU{
			Name:  s.GetOID(v.OID, -1),
			Type:  v.Type,
			Value: v.Value,
		})
	}

	for _, t := range s.Trap {
		_, err := t.SendTrap(trap)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *SNMP) SetDevice(device Device) {
	s.Device = device
}

func (s *SNMP) SetSerialSend(fun func(value string)) {
	s.TtySend = fun
}

// 关闭 SNMP 服务器。
func (s *SNMP) Close() {
	s.Server.Shutdown()
}

// 启动 SNMP 服务器。
func (s *SNMP) Run() {
	listen := fmt.Sprintf("%s:%d", s.Config.Address, s.Config.Port)
	s.Master.Logger.Infof("SNMP server is running on %s", listen)
	s.Server.ServeForever()
}

func (s *SNMP) AddPublicOID(oid *GoSNMPServer.PDUValueControlItem) {
	s.Public.OIDs = append(s.Public.OIDs, oid)
}

func (s *SNMP) AddPrivateOID(oid *GoSNMPServer.PDUValueControlItem) {
	if s.Private == nil {
		return
	}
	s.Private.OIDs = append(s.Private.OIDs, oid)
}

// 获取 OID。
// name: 服务名。
// count: 索引。-1: 不带索引。其他: 带索引。
func (s *SNMP) GetOID(name string, count int) string {
	if strings.HasPrefix(name, ".") {
		return name
	}
	oid, err := s.Mib.OID(name)
	if err != nil {
		panic(err)
	}
	if count == -1 {
		return fmt.Sprintf(".%s", oid.String())
	}
	return fmt.Sprintf(".%s.%d", oid.String(), count)
}

func (s *SNMP) Apply() {
	s.Public.SyncConfig()
}

// 添加一个表。
// name: 服务名。
// obj: 对象。
// count: 表的行数。
// onGet: 获取数据的回调函数。
func (s *SNMP) AddTable(name string, obj any, count int, tp gosnmp.Asn1BER, onGet func(obj any, index int) (any, error)) {
	for i := 0; i < count; i++ {
		index := i + 1
		s.Public.OIDs = append(s.Public.OIDs, &GoSNMPServer.PDUValueControlItem{
			OID:  s.GetOID(name, index),
			Type: tp,
			OnGet: func() (interface{}, error) {
				return onGet(obj, index)
			},
		})
	}
}

// 移除所有表。
// name: 服务名。
func (s *SNMP) RemoveAllTable(name string) {
	oid := s.GetOID(name, -1)
	for i := 0; i < len(s.Public.OIDs); i++ {
		// master oid starts with oid
		if strings.HasPrefix(s.Public.OIDs[i].OID, oid) {
			s.Public.OIDs = append(s.Public.OIDs[:i], s.Public.OIDs[i+1:]...)
			i--
		}
	}
}

// 移除表。
// name: 服务名。
// index: 索引。
func (s *SNMP) RemoveTable(name string, index int) {
	oid := s.GetOID(name, index)
	for i := 0; i < len(s.Public.OIDs); i++ {
		if s.Public.OIDs[i].OID == oid {
			s.Public.OIDs = append(s.Public.OIDs[:i], s.Public.OIDs[i+1:]...)
			break
		}
	}
}
