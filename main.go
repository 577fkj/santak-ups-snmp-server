package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gosnmp/gosnmp"
	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"github.com/rifflock/lfshook"
	"github.com/sirupsen/logrus"
	"github.com/slayercat/GoSNMPServer"
	"github.com/spf13/pflag"
	"go.bug.st/serial/enumerator"
	"gopkg.in/yaml.v3"
)

type User struct {
	Username  string `yaml:"username"`
	PrivPass  string `yaml:"privpass"`
	AuthPass  string `yaml:"authpass"`
	AuthProto string `yaml:"authproto"`
	PrivProto string `yaml:"privproto"`
}

type Trap struct {
	Enable    bool   `yaml:"enable"`
	Host      string `yaml:"host"`
	Port      int    `yaml:"port"`
	Community string `yaml:"community"`
	User      User   `yaml:"user"`

	Version gosnmp.SnmpVersion `yaml:"version"`
}

type Snmp struct {
	PublicName  string `yaml:"public"`
	PrivateName string `yaml:"private"`

	User []User `yaml:"user"`

	Trap []Trap `yaml:"trap"`

	LogLevel string `yaml:"log-level"`
}

type RunConfig struct {
	COMPort string `yaml:"com-port"`

	Address string `yaml:"address"`
	Port    int    `yaml:"port"`

	Snmp Snmp `yaml:"snmp"`

	DisableBuzz bool `yaml:"disable-buzz"`

	LogLevel  string   `yaml:"log-level"`
	LogFilter []string `yaml:"log-filter"`
}

var defaultConfig = RunConfig{
	COMPort: "COM8",
	Address: "0.0.0.0",
	Port:    161,

	Snmp: Snmp{
		PublicName:  "public",
		PrivateName: "private",

		User: []User{
			{
				Username:  "test",
				PrivPass:  "test",
				AuthPass:  "test",
				AuthProto: "MD5",
				PrivProto: "AES",
			},
		},

		Trap: []Trap{
			{
				Enable:    true,
				Host:      "192.168.1.1",
				Port:      162,
				Community: "public",
				Version:   gosnmp.Version2c,
				User: User{
					Username:  "test",
					PrivPass:  "test",
					AuthPass:  "test",
					AuthProto: "MD5",
					PrivProto: "AES",
				},
			},
		},

		LogLevel: "error",
	},

	DisableBuzz: false,
	LogLevel:    "info",
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

var config = RunConfig{}

func argsParse() {
	var configPath string
	pflag.StringVarP(&configPath, "config", "c", "config.yml", "配置文件路径 (可选)")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		config = defaultConfig
		// 保存默认配置到文件
		dataBytes, err := yaml.Marshal(config)
		if err != nil {
			fmt.Println("解析配置文件失败：", err)
			return
		}
		err = os.WriteFile(configPath, dataBytes, 0644)
		if err != nil {
			fmt.Println("保存配置文件失败：", err)
			return
		}
	} else {
		dataBytes, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Println("读取文件失败：", err)
			return
		}
		err = yaml.Unmarshal(dataBytes, &config)
		if err != nil {
			fmt.Println("解析配置文件失败：", err)
			return
		}
	}

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

func setLogLevel(log *logrus.Logger, level string) {
	lvl, err := logrus.ParseLevel(level)
	if err != nil {
		panic("Log level error: " + err.Error())
	}
	log.SetLevel(lvl)
}

func main() {
	sigs = make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)

	argsParse()
	setLogLevel(Logger, config.LogLevel)
	setLogLevel(SNMPLogger, config.Snmp.LogLevel)

	var words []string
	for _, key := range config.LogFilter {
		if key == "" {
			continue
		}
		words = append(words, strings.TrimSpace(key))
	}
	Logger.Infof("Filter words: %s", strings.Join(words, ", "))
	filter.FilterWords = words

	var auth []SNMPAuth
	for _, user := range config.Snmp.User {
		auth = append(auth, SNMPAuth{
			Username:  user.Username,
			AuthKey:   user.AuthPass,
			PrivKey:   user.PrivPass,
			AuthProto: getAuthProto(user.AuthProto),
			PrivProto: getPrivProto(user.PrivProto),
		})
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
		PublicName:  config.Snmp.PublicName,
		PrivateName: config.Snmp.PrivateName,

		Address: config.Address,
		Port:    config.Port,

		Auth: auth,

		SetCallback: device.SetCallback,

		Logger: GoSNMPServer.WrapLogrus(SNMPLogger),
	}, device.EnableService, data)
	snmp.SetDevice(device)
	snmp.SetSerialSend(createSerialSend(serial))

	for _, trap := range config.Snmp.Trap {
		if trap.Enable {
			config := TrapConfig{
				Host:      trap.Host,
				Port:      uint16(trap.Port),
				Community: trap.Community,
				Version:   trap.Version,
			}

			if trap.User.Username != "" && trap.User.AuthPass != "" && trap.User.PrivPass != "" {
				config.Auth = &SNMPAuth{
					Username: trap.User.Username,
					AuthKey:  trap.User.AuthPass,
					PrivKey:  trap.User.PrivPass,

					AuthProto: getAuthProto(trap.User.AuthProto),
					PrivProto: getPrivProto(trap.User.PrivProto),
				}
			}

			snmp.AddTrap(config)
		}
	}

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

	// snmp.Apply()

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

var filter = &ContentFilterHook{}

// ContentFilterHook 用于过滤包含特定关键字的日志
type ContentFilterHook struct {
	FilterWords []string
}

// Levels 定义 Hook 适用于哪些日志级别
func (hook *ContentFilterHook) Levels() []logrus.Level {
	return []logrus.Level{
		logrus.InfoLevel,
	}
}

// Fire 根据日志内容进行过滤
func (hook *ContentFilterHook) Fire(entry *logrus.Entry) error {
	if len(hook.FilterWords) == 0 {
		return nil
	}

	for _, word := range hook.FilterWords {
		if strings.Contains(entry.Message, word) {
			// replce entry with emtpy one to discard message
			*entry = logrus.Entry{
				Level: logrus.TraceLevel,
				Logger: &logrus.Logger{
					Out:       io.Discard,
					Formatter: &logrus.JSONFormatter{},
				},
			}
			return nil
		}
	}
	return nil
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

	logger.AddHook(filter)

	logger.AddHook(hook)
	return logger
}
