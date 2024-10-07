package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"

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
	Manufacturer    string `snmp:"upsIdentManufacturer"`         // 制造商
	Model           string `snmp:"upsIdentModel"`                // 型号
	SoftwareVersion string `snmp:"upsIdentUPSSoftwareVersion"`   // UPS软件版本
	AgentVersion    string `snmp:"upsIdentAgentSoftwareVersion"` // Agent软件版本
	Name            string `snmp:"upsIdentName,w"`               // 名称
	AttachedDevices string `snmp:"upsIdentAttachedDevices,w"`    // 连接设备
}

type SNMPDataBattery struct { // 电池信息
	Status  int `snmp:"upsBatteryStatus"`             // 状态 1: unknown, 2: batteryNormal, 3: batteryLow, 4: batteryDepleted
	Seconds int `snmp:"upsSecondsOnBattery"`          // 已经在电池上运行的时间
	Minutes int `snmp:"upsEstimatedMinutesRemaining"` // 估计剩余时间(分钟)
	Charge  int `snmp:"upsEstimatedChargeRemaining"`  // 估计剩余电量(%) 0-100
	Voltage int `snmp:"upsBatteryVoltage"`            // 电池电压
	Current int `snmp:"upsBatteryCurrent"`            // 电池电流
	Temp    int `snmp:"upsBatteryTemperature"`        // 电池温度
}

type SNMPDataInput struct { // 输入信息
	LineBads int `snmp:"upsInputLineBads"` // 输入线路故障数
	NumLines int `snmp:"upsInputNumLines"` // 输入线路数

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
	Source   int `snmp:"upsOutputSource"`    // 输出源 1: other, 2: none, 3: normal, 4: bypass, 5: battery, 6: booster, 7: reducer
	Freq     int `snmp:"upsOutputFrequency"` // 输出频率
	NumLines int `snmp:"upsOutputNumLines"`  // 输出线路数

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
	Freq     int `snmp:"upsBypassFrequency"`
	NumLines int `snmp:"upsBypassNumLines"`

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
	// upsAlarmTime    int
}

type SNMPDataTest struct {
	Id             string    `snmp:"upsTestId,w"`           // 当前测试ID
	SpinLock       int       `snmp:"upsTestSpinLock,w"`     // 测试锁，自旋锁
	ResultsSummary int       `snmp:"upsTestResultsSummary"` // 测试状态 1: done, 2: done Warn, 3: done Error, 4: aborted, 5: in progress, 6: noRun
	ResultsDetail  string    `snmp:"upsTestResultsDetail"`  // 测试结果
	StartTime      TimesTamp `snmp:"upsTestStartTime"`      // 测试开始时间
	ElapsedTime    int       `snmp:"upsTestElapsedTime"`    // 测试持续时间
}

type SNMPDataControl struct {
	ShutdownType   int `snmp:"upsShutdownType,w"`       // 1: output, 2: system
	ShutdownAfter  int `snmp:"upsShutdownAfterDelay,w"` // 关机延迟时间
	StartupAfter   int `snmp:"upsStartupAfterDelay,w"`  // 启动延迟时间
	RebootDuration int `snmp:"upsRebootWithDuration,w"` // 重启持续时间
	AutoRestart    int `snmp:"upsAutoRestart,w"`        // 1: on, 2: off
}

type SNMPDataConfig struct {
	InputVoltage             int `snmp:"upsConfigInputVoltage,w"`
	InputFreq                int `snmp:"upsConfigInputFreq,w"`
	OutputVoltage            int `snmp:"upsConfigOutputVoltage,w"`
	OutputFreq               int `snmp:"upsConfigOutputFreq,w"`
	OutputVA                 int `snmp:"upsConfigOutputVA"`
	OutputPower              int `snmp:"upsConfigOutputPower"`
	LowBatteryTime           int `snmp:"upsConfigLowBattTime,w"`
	AudibleStatus            int `snmp:"upsConfigAudibleStatus,w"` // 蜂鸣器 1: disable, 2: enable, 3: mute
	LowVoltageTransferPoint  int `snmp:"upsConfigLowVoltageTransferPoint,w"`
	HighVoltageTransferPoint int `snmp:"upsConfigHighVoltageTransferPoint,w"`
}

type SNMPData struct {
	Ident   *SNMPDataIdent   `snmp:"upsIdent"`
	Battery *SNMPDataBattery `snmp:"upsBattery"`
	Input   *SNMPDataInput   `snmp:"upsInput"`
	Output  *SNMPDataOutput  `snmp:"upsOutput"`
	Bypass  *SNMPDataBypass  `snmp:"upsBypass"`
	Alarm   *SNMPDataAlarm   `snmp:"upsAlarm"`
	Test    *SNMPDataTest    `snmp:"upsTest"`
	Control *SNMPDataControl `snmp:"upsControl"`
	Config  *SNMPDataConfig  `snmp:"upsConfig"`

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

	Server  *GoSNMPServer.SNMPServer
	Master  *GoSNMPServer.MasterAgent
	Public  *GoSNMPServer.SubAgent
	Private *GoSNMPServer.SubAgent
	Mib     *smi.MIB
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
			// 解析snmp标签
			parts := strings.Split(tag, ",")
			id := parts[0]    // 第一个部分为OID
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
				Id:        id,
				FieldType: getTypeName(field.Type),
				Writable:  writable,
				SNMPType:  snmpType,
			})
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

	Auth *SNMPAuth

	SetCallback func(snmp *SNMP, name string, value interface{}) error
}

func snmp_server(config SNMPConfig, server_enable SNMPData, data *SNMPData) *SNMP {
	path, err := os.Getwd()
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}

	mib := smi.NewMIB(filepath.Join(path, "mibs"))
	err = mib.LoadModules("UPS-MIB")
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}

	snmp := &SNMP{
		Data:   data,
		Config: &config,
		Mib:    mib,
	}

	public := GoSNMPServer.SubAgent{
		CommunityIDs: []string{config.PublicName},
	}

	private := GoSNMPServer.SubAgent{
		CommunityIDs: []string{config.PrivateName},
	}

	master := GoSNMPServer.MasterAgent{
		SecurityConfig: GoSNMPServer.SecurityConfig{
			AuthoritativeEngineBoots: 1,
			Users:                    []gosnmp.UsmSecurityParameters{},
		},
		SubAgents: []*GoSNMPServer.SubAgent{&public, &private},
	}

	if config.Logger != nil {
		master.Logger = config.Logger
	} else {
		master.Logger = GoSNMPServer.NewDefaultLogger()
	}

	ids := getFieldInfoFromType(reflect.TypeOf(SNMPData{}))
	var currentData any
	var currentEnable any
	currentData = data
	currentEnable = server_enable
	for _, id := range ids {
		m_id := id.Id
		name := id.FieldName
		type_name := id.FieldType

		// 获取字段的值
		field := reflect.ValueOf(currentData)
		if field.Kind() == reflect.Ptr {
			field = field.Elem() // 如果是指针，先解引用
		}
		field = field.FieldByName(name)

		// 获取字段的值
		enableField := reflect.ValueOf(currentEnable)
		if enableField.Kind() == reflect.Ptr {
			enableField = enableField.Elem() // 如果是指针，先解引用
		}

		enableField = enableField.FieldByName(name)

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
			fmt.Println("unsupported type:", type_name)
			currentData = reflect.ValueOf(data).Elem().FieldByName(name).Interface()
			currentEnableObj := reflect.ValueOf(server_enable).FieldByName(name)
			if currentEnableObj.Kind() == reflect.Ptr {
				currentEnableObj = currentEnableObj.Elem()
			}
			if currentEnableObj.IsValid() {
				currentEnable = reflect.ValueOf(server_enable).FieldByName(name).Interface()
			} else {
				currentEnable = server_enable
			}
			continue
		}

		if !enableField.IsValid() || enableField.IsZero() {
			fmt.Printf("Skip service [%s](%s) %s\n", name, m_id, oid.String())
			continue
		}

		oid_str := fmt.Sprintf(".%s.0", oid.String())

		fmt.Printf("Add service [%s](%s) %s\n", name, m_id, oid_str)

		if !id.Writable {
			onSet := func(value interface{}) error {
				if !field.IsValid() {
					return fmt.Errorf("field not found")
				}
				field.Set(reflect.ValueOf(value))
				if config.SetCallback != nil {
					return config.SetCallback(snmp, name, value)
				}
				return nil
			}
			private.OIDs = append(private.OIDs, &GoSNMPServer.PDUValueControlItem{
				OID:   oid_str,
				Type:  tp,
				OnSet: onSet,
			})
		}
		public.OIDs = append(public.OIDs, &GoSNMPServer.PDUValueControlItem{
			OID:  oid_str,
			Type: tp,
			OnGet: func() (interface{}, error) {
				fmt.Println("Get:", name)
				if !field.IsValid() {
					return nil, fmt.Errorf("field not found")
				}
				fmt.Println("Get data:", field.Interface())
				return field.Interface(), nil
			},
		})
	}

	if config.Auth != nil {
		master.SecurityConfig.Users = []gosnmp.UsmSecurityParameters{
			{
				UserName:                 config.Auth.Username,
				AuthenticationProtocol:   config.Auth.AuthProto,
				PrivacyProtocol:          config.Auth.PrivProto,
				AuthenticationPassphrase: config.Auth.AuthKey,
				PrivacyPassphrase:        config.Auth.PrivKey,
			},
		}
	}

	listen := fmt.Sprintf("%s:%d", config.Address, config.Port)

	// 创建并启动服务器
	server := GoSNMPServer.NewSNMPServer(master)
	err = server.ListenUDP("udp", listen)
	if err != nil {
		log.Fatalf("Error in listen: %+v", err)
	}

	snmp.Server = server
	snmp.Master = &master
	snmp.Public = &public
	snmp.Private = &private

	return snmp
}

// 关闭 SNMP 服务器。
func (s *SNMP) Close() {
	s.Server.Shutdown()
}

// 启动 SNMP 服务器。
func (s *SNMP) Run() {
	listen := fmt.Sprintf("%s:%d", s.Config.Address, s.Config.Port)
	log.Printf("SNMP server is running on %s\n", listen)
	s.Server.ServeForever()
}

func (s *SNMP) AddPublicOID(oid *GoSNMPServer.PDUValueControlItem) {
	s.Public.OIDs = append(s.Public.OIDs, oid)
	s.Public.SyncConfig()
}

func (s *SNMP) AddPrivateOID(oid *GoSNMPServer.PDUValueControlItem) {
	s.Private.OIDs = append(s.Private.OIDs, oid)
	s.Private.SyncConfig()
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

// 添加一个表。
// name: 服务名。
// obj: 对象。
// count: 表的行数。
// onGet: 获取数据的回调函数。
func (s *SNMP) AddTable(name string, obj any, count int, tp gosnmp.Asn1BER, onGet func(obj any, index int) (any, error)) {
	for i := 0; i < count; i++ {
		index := i
		s.Public.OIDs = append(s.Public.OIDs, &GoSNMPServer.PDUValueControlItem{
			OID:  s.GetOID(name, index),
			Type: tp,
			OnGet: func() (interface{}, error) {
				return onGet(obj, index)
			},
		})
	}
	s.Public.SyncConfig()
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
	s.Public.SyncConfig()
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
	s.Public.SyncConfig()
}
