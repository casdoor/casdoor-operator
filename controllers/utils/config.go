package utils

import "strings"

func MergeAppConf(newConf map[string]string) string {
	defaultConf := map[string]string{
		"appname":                 "casdoor",
		"httpport":                "8000",
		"runmode":                 "dev",
		"copyrequestbody":         "true",
		"driverName":              "mysql",
		"dataSourceName":          "root:123456@tcp(localhost:3306)/",
		"dbName":                  "casdoor",
		"tableNamePrefix":         "",
		"showSql":                 "false",
		"redisEndpoint":           "",
		"defaultStorageProvider":  "",
		"isCloudIntranet":         "false",
		"authState":               "casdoor",
		"socks5Proxy":             "127.0.0.1:10808",
		"verificationCodeTimeout": "10",
		"initScore":               "2000",
		"logPostOnly":             "true",
		"origin":                  "",
		"staticBaseUrl":           "https://cdn.casbin.org",
	}
	for key, value := range newConf {
		defaultConf[key] = value
	}
	var builder strings.Builder
	for key, value := range defaultConf {
		builder.WriteString(key)
		builder.WriteString(" = ")
		builder.WriteString(value)
		builder.WriteString("\n")
	}
	return builder.String()
}
