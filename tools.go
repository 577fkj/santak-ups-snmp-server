package main

import (
	"time"

	"github.com/gosnmp/gosnmp"
)

var startTime = time.Now()

func getRunningTimeInSeconds() float64 {
	return time.Since(startTime).Seconds()
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
