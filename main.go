package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"github.com/rifflock/lfshook"
	"github.com/sirupsen/logrus"
	"github.com/slayercat/GoSNMPServer"
	"github.com/spf13/pflag"
	"go.bug.st/serial/enumerator"
)

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

var sigs chan os.Signal

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

func main() {
	sigs = make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)

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

	serial, err := serialInit(TTYConfig{
		Port:     config.COMPort,
		Received: serialReceived,
	})
	if err != nil {
		Logger.Fatalf("Init serail faild: %s", err.Error())
		return
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
	snmp.SetDevice(device)
	snmp.SetSerialSend(createSerialSend(serial))

	alarm.SetSNMP(snmp)

	device.InitCallback(snmp, data)

	serial.SetUserData(snmp)

	go func() {
		for {
			select {
			case <-sigs:
				Logger.Infof("Received signal. Stopping send operation...")
				return
			default:
				serial.Send(device.GetInfo)
				serial.Send(device.GetRated)
				serial.Send(device.GetManufacturer)
				serial.Send(device.ExtraGetInfo)
				serial.Send(device.ExtraGetError)
				serial.Send(device.ExtraGetTPInfo)
				serial.Send(device.ExtraGetRated)

				time.Sleep(time.Second * 1)
			}
		}
	}()

	go func() {
		<-sigs
		Logger.Info("Received signal. Stopping...")
		err := serial.Close()
		if err != nil {
			Logger.Fatalf("Serial close faild: %s", err.Error())
		}
		snmp.Close()
		os.Exit(0)
	}()

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

	// go runNCM()

	snmp.Run()
}

func runNCM() {
	// Santak NMC Card
	// port 2993 Santak NMC
	// port 4679 DELL
	// req <SCAN_REQUEST/>
	// rep <SCAN macAddress="00:1A:2B:3C:4D:5E"/>
	// 监听 UDP 地址和端口
	addr := net.UDPAddr{
		Port: 2993,                   // 设置服务器监听的端口
		IP:   net.ParseIP("0.0.0.0"), // 监听所有网络接口
	}

	// 创建 UDP 连接
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		fmt.Println("Error starting UDP server:", err)
		return
	}
	defer conn.Close()
	fmt.Println("UDP server is listening on port 2993...")

	// 缓冲区，用于存放接收到的数据
	buffer := make([]byte, 1024)

	// 循环读取来自客户端的消息
	for {
		// 读取 UDP 数据包
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("Error reading from UDP:", err)
			continue
		}

		// 输出接收到的消息
		fmt.Printf("Received message from %s: %s\n", clientAddr.String(), string(buffer[:n]))

		if string(buffer[:n]) == "<SCAN_REQUEST/>" {
			// 发送消息给客户端
			response := []byte("<SCAN macAddress=\"00:1A:2B:3C:4D:5E\"/>")
			_, err = conn.WriteToUDP(response, clientAddr)
			if err != nil {
				fmt.Println("Error sending response:", err)
				continue
			}
		}
	}
}

func init() {
	Logger = newLog("app")
	SNMPLogger = newLog("snmp")
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
