package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"github.com/rifflock/lfshook"
	"github.com/sirupsen/logrus"
	"github.com/slayercat/GoSNMPServer"
	"github.com/spf13/pflag"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

type Alarm struct {
	Alarms []AlarmEntry
	Snmp   *SNMP

	NeedApply bool
}

func init() {
	Logger = newLog("app")
	SNMPLogger = newLog("snmp")
}

var startTime = time.Now()

func getRunningTimeInSeconds() float64 {
	return time.Since(startTime).Seconds()
}

var data = &SNMPData{
	Ident:   &SNMPDataIdent{},
	Battery: &SNMPDataBattery{},
	Input:   &SNMPDataInput{},
	Output:  &SNMPDataOutput{},
	Bypass:  &SNMPDataBypass{},
	Alarm:   &SNMPDataAlarm{},
	Test:    &SNMPDataTest{},
	Control: &SNMPDataControl{},
	Config:  &SNMPDataConfig{},
}

var alarm = Alarm{}

var Logger *logrus.Logger
var SNMPLogger *logrus.Logger

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

func (a *Alarm) RemoveWithDesc(desc string) {
	desc = a.getOID(desc)
	for i, alarm := range a.Alarms {
		if alarm.Descr == desc {
			a.Remove(i)
			a.NeedApply = true
			return
		}
	}
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
}

func tty(snmp *SNMP, device Device) func(cmd string) {
	mode := &serial.Mode{
		BaudRate: 2400,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	Logger.Infof("try open port: %s", config.COMPort)

	s, err := serial.Open(config.COMPort, mode)
	if err != nil {
		Logger.Fatal(err.Error())
	}

	result := make([]byte, 0)
	buf := make([]byte, 128)

	send := func(cmd string) {
		if cmd == "" {
			return
		}
		_, err = s.Write([]byte(cmd + "\r"))
		if err != nil {
			Logger.Errorf("send err: %s", err.Error())
		}
	}

	go func() {
		for {
			send(device.GetInfo)
			send(device.GetRated)
			send(device.GetManufacturer)
			send(device.ExtraGetInfo)
			send(device.ExtraGetError)
			send(device.ExtraGetTPInfo)
			send(device.ExtraGetRated)

			time.Sleep(time.Second * 1)
		}
	}()

	go func() {
		for {
			for {
				n, err := s.Read(buf[0:])
				if err != nil {
					Logger.Errorf("read err: %s", err.Error())
					break
				}
				if string(buf[0:n]) == "\r" {
					break
				}
				result = append(result, buf[:n]...)
			}
			if len(result) == 0 {
				continue
			}
			Logger.Debugf("tty recv: %s", string(result))
			err = device.OnReceive(snmp, data, string(result))
			if err != nil {
				Logger.Errorf("OnReceive err: %s", err.Error())
			}
			result = result[:0]
		}
	}()

	return send
}

type RunArgs struct {
	COMPort string
	Address string
	Port    int

	// SNMP
	PublicName  string
	PrivateName string
	Username    string
	PrivPass    string
	AuthPass    string
	AuthProto   string
	PrivProto   string

	DisableBuzz bool

	LogLevel string
}

var config = RunArgs{}

func argsParse() {
	pflag.StringVarP(&config.COMPort, "com", "c", "", "串口设备 [COM8, /dev/ttyUSB0]")
	pflag.StringVarP(&config.Address, "address", "a", "0.0.0.0", "监听地址 (可选)")
	pflag.IntVarP(&config.Port, "port", "p", 161, "监听端口 (可选)")

	pflag.StringVarP(&config.PublicName, "public", "P", "public", "SNMPv1/v2c 公共名 (可选)")
	pflag.StringVarP(&config.PrivateName, "private", "R", "private", "SNMPv1/v2c 私有名 (可选)")
	pflag.StringVarP(&config.Username, "username", "u", "admin", "SNMPv3 用户名 (可选)")
	pflag.StringVarP(&config.AuthPass, "authpass", "A", "admin", "SNMPv3 认证密码 (可选)")
	pflag.StringVarP(&config.PrivPass, "privpass", "V", "admin", "SNMPv3 加密密码 (可选)")
	pflag.StringVarP(&config.AuthProto, "authproto", "t", "MD5", "SNMPv3 认证协议 [MD5, SHA, SHA224, SHA256, SHA384, SHA512] (可选)")
	pflag.StringVarP(&config.PrivProto, "privproto", "i", "DES", "SNMPv3 加密协议 [DES, AES, AES192, AES192C, AES256, AES256C] (可选)")

	pflag.BoolVarP(&config.DisableBuzz, "disable-buzz", "b", false, "禁用蜂鸣器 (可选)")

	pflag.StringVarP(&config.LogLevel, "log", "l", "info", "日志级别 [trace, debug, info, warn, error, fatal] (可选)")

	pflag.Parse()

	if config.COMPort == "" {
		pflag.Usage()

		ports, err := enumerator.GetDetailedPortsList()
		if err != nil {
			Logger.Fatal(err.Error())
		}
		if len(ports) == 0 {
			fmt.Println("No serial ports found!")
			return
		}
		for _, port := range ports {
			fmt.Printf("Found port: %s\n", port.Name)
			if port.IsUSB {
				fmt.Printf("   USB ID     %s:%s\n", port.VID, port.PID)
				fmt.Printf("   USB serial %s\n", port.SerialNumber)
			}
		}

		os.Exit(1)
	}
}

func getAuthProto(proto string) gosnmp.SnmpV3AuthProtocol {
	switch proto {
	case "MD5":
		return gosnmp.MD5
	case "SHA":
		return gosnmp.SHA
	case "SHA224":
		return gosnmp.SHA224
	case "SHA256":
		return gosnmp.SHA256
	case "SHA384":
		return gosnmp.SHA384
	case "SHA512":
		return gosnmp.SHA512
	}
	return gosnmp.NoAuth
}

func getPrivProto(proto string) gosnmp.SnmpV3PrivProtocol {
	switch proto {
	case "DES":
		return gosnmp.DES
	case "AES":
		return gosnmp.AES
	case "AES192":
		return gosnmp.AES192
	case "AES192C":
		return gosnmp.AES192C
	case "AES256":
		return gosnmp.AES256
	case "AES256C":
		return gosnmp.AES256C
	}
	return gosnmp.NoPriv
}

func main() {
	argsParse()
	lvl, err := logrus.ParseLevel(config.LogLevel)
	if err != nil {
		panic("Log level error: " + err.Error())
	}
	Logger.SetLevel(lvl)
	SNMPLogger.SetLevel(lvl)

	var auth SNMPAuth
	if config.Username != "" && config.AuthPass != "" && config.PrivPass != "" {
		auth = SNMPAuth{
			Username:  config.Username,
			AuthKey:   config.AuthPass,
			PrivKey:   config.PrivPass,
			AuthProto: getAuthProto(config.AuthProto),
			PrivProto: getPrivProto(config.PrivProto),
		}
	}

	device := Mt1000Pro

	snmp := snmp_server(SNMPConfig{
		PublicName:  config.PublicName,
		PrivateName: config.PrivateName,

		Address: config.Address,
		Port:    config.Port,

		Auth: &auth,

		SetCallback: device.SetCallback,

		Logger: GoSNMPServer.WrapLogrus(SNMPLogger),
	}, device.EnableService, data)
	snmp.TtySend = tty(snmp, device)
	snmp.Device = device
	device.InitCallback(snmp, data)

	alarm.Snmp = snmp

	// snmp.AddPublicOID(&GoSNMPServer.PDUValueControlItem{
	// 	OID:  ".1.3.6.1.2.1.1.1.0",
	// 	Type: gosnmp.OctetString,
	// 	OnGet: func() (value interface{}, err error) {
	// 		return "UPS-System", nil
	// 	},
	// })

	// snmp.AddPublicOID(&GoSNMPServer.PDUValueControlItem{
	// 	OID:  ".1.3.6.1.2.1.1.2.0",
	// 	Type: gosnmp.ObjectIdentifier,
	// 	OnGet: func() (value interface{}, err error) {
	// 		return ".1.3.6.1.2.1.33", nil
	// 	},
	// })

	snmp.Run()

	// Santak NMC Card
	// port 2993 Santak NMC
	// port 4679 DELL
	// req <SCAN_REQUEST/>
	// rep <SCAN macAddress="00:1A:2B:3C:4D:5E"/>
	// // 监听 UDP 地址和端口
	// addr := net.UDPAddr{
	// 	Port: 2993,                   // 设置服务器监听的端口
	// 	IP:   net.ParseIP("0.0.0.0"), // 监听所有网络接口
	// }

	// // 创建 UDP 连接
	// conn, err := net.ListenUDP("udp", &addr)
	// if err != nil {
	// 	fmt.Println("Error starting UDP server:", err)
	// 	return
	// }
	// defer conn.Close()
	// fmt.Println("UDP server is listening on port 2993...")

	// // 缓冲区，用于存放接收到的数据
	// buffer := make([]byte, 1024)

	// // 循环读取来自客户端的消息
	// for {
	// 	// 读取 UDP 数据包
	// 	n, clientAddr, err := conn.ReadFromUDP(buffer)
	// 	if err != nil {
	// 		fmt.Println("Error reading from UDP:", err)
	// 		continue
	// 	}

	// 	// 输出接收到的消息
	// 	fmt.Printf("Received message from %s: %s\n", clientAddr.String(), string(buffer[:n]))

	// 	if string(buffer[:n]) == "<SCAN_REQUEST/>" {
	// 		// 发送消息给客户端
	// 		response := []byte("<SCAN macAddress=\"00:1A:2B:3C:4D:5E\"/>")
	// 		_, err = conn.WriteToUDP(response, clientAddr)
	// 		if err != nil {
	// 			fmt.Println("Error sending response:", err)
	// 			continue
	// 		}
	// 	}
	// }
}

func newLog(name string) *logrus.Logger {
	// 创建一个 writer
	logWriter, err := rotatelogs.New(
		filepath.Join("logs", name+"Log_%Y-%m-%d.log"), //日志路径
		rotatelogs.WithLinkName(filepath.Join("logs", name+"Log.log")),
		rotatelogs.WithRotationTime(24*time.Hour), // 每 24 小时轮转一次
		rotatelogs.WithRotationSize(10*1024*1024), // 当日志文件超过 10MB 时轮转
	)
	if err != nil {
		panic(err)
	}

	// 创建一个 Error 级别的 writer
	errorWriter, err := rotatelogs.New(
		filepath.Join("logs", name+"Error_%Y-%m-%d.log"), //日志路径
		rotatelogs.WithLinkName(filepath.Join("logs", name+"Error.log")),
		rotatelogs.WithRotationTime(24*time.Hour), // 每 24 小时轮转一次
		rotatelogs.WithRotationSize(10*1024*1024), // 当日志文件超过 10MB 时轮转
	)
	if err != nil {
		panic(err)
	}

	// 新建Hook，按日志级别匹配 writer
	hook := lfshook.NewHook(
		lfshook.WriterMap{
			logrus.DebugLevel: logWriter,
			logrus.InfoLevel:  logWriter,
			logrus.WarnLevel:  logWriter,
			logrus.TraceLevel: logWriter,

			logrus.ErrorLevel: errorWriter,
			logrus.FatalLevel: errorWriter,
			logrus.PanicLevel: errorWriter,
		},
		&logrus.TextFormatter{
			FullTimestamp: true,
			ForceColors:   true,
		},
	)

	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
		ForceColors:   true,
	})
	logger.AddHook(hook)
	return logger
}
