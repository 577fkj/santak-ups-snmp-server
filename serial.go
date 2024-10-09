package main

import "go.bug.st/serial"

type TTYConfig struct {
	Port string

	Received func(userData any, value string)
}

type TTY struct {
	Serial   serial.Port
	UserData any
}

func (tty *TTY) Close() error {
	return tty.Serial.Close()
}

func (tty *TTY) SetUserData(value any) {
	tty.UserData = value
}

func (tty *TTY) Send(value string) error {
	if value == "" {
		return nil
	}
	_, err := tty.Serial.Write([]byte(value + "\r"))
	return err
}

func serialReadLine(tty *TTY) string {
	buf := make([]byte, 128)
	result := make([]byte, 0)
	for {
		n, err := tty.Serial.Read(buf[0:])
		if err != nil {
			if err.Error() == "The handle is invalid." {
				return ""
			}
			Logger.Errorf("read err: %s", err.Error())
			break
		}
		if string(buf[0:n]) == "\r" {
			break
		}
		result = append(result, buf[:n]...)
	}
	if len(result) == 0 {
		return ""
	}
	Logger.Debugf("tty recv: %s", string(result))
	return string(result)
}

func serialInit(config TTYConfig) (*TTY, error) {
	mode := &serial.Mode{
		BaudRate: 2400,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	Logger.Infof("Try open port: '%s'", config.Port)

	s, err := serial.Open(config.Port, mode)
	if err != nil {
		Logger.Fatalf("Open port faild: %s", err.Error())
		return nil, err
	}

	ret := &TTY{
		Serial: s,
	}

	go func() {
		for {
			select {
			case <-sigs:
				Logger.Infof("Received signal. Stopping read operation...")
				return
			default:
				result := serialReadLine(ret)
				if len(result) != 0 {
					config.Received(ret.UserData, result)
				}
			}
		}
	}()

	return ret, nil
}

func createSerialSend(tty *TTY) func(value string) {
	return func(value string) {
		tty.Send(value)
	}
}

func serialReceived(userData any, value string) {
	snmp := userData.(*SNMP)
	err := snmp.Device.OnReceive(snmp, data, value)
	if err != nil {
		Logger.Errorf("OnReceive err: %s", err.Error())
	}
}
