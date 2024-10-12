package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

var startTime = time.Now()

func getRunningTimeInSeconds() float64 {
	return time.Since(startTime).Seconds()
}

// ExtractEnterpriseIDAndSpecificTrap 解析 Trap OID 并提取企业 ID 和 SpecificTrap
func ExtractEnterpriseIDAndSpecificTrap(oid string) (string, int, error) {
	// 将 OID 分割成各个部分
	parts := strings.Split(oid, ".")
	if len(parts) < 2 {
		return "", 0, fmt.Errorf("无效的 OID: %s", oid)
	}

	// 检查 OID 是否满足基本格式：企业 OID 以 ".1.3.6.1.4.1" 开头
	// "1.3.6.1.4.1" 是企业 OID 的标准前缀
	// "1.3.6.1.2.1" 是 IANA OID 的标准前缀
	if !strings.HasPrefix(oid, "1.3.6.1.4.1") && !strings.HasPrefix(oid, "1.3.6.1.2.1") {
		return "", 0, fmt.Errorf("OID 不是企业特定的 OID: %s", oid)
	}

	// 提取企业 ID，去掉最后两部分（Trap 的 ".0.x" 部分）
	enterpriseID := strings.Join(parts[:len(parts)-2], ".")

	// 提取 SpecificTrap，最后一位是 Trap 编号
	specificTrap, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return "", 0, fmt.Errorf("无法解析 SpecificTrap 值: %v", err)
	}

	return enterpriseID, specificTrap, nil
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
