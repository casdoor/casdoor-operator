package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"github.com/imdario/mergo"
	"math/big"
	"strconv"
	"strings"
	"time"
)

func MergeAppConf(newConf map[string]string) (string, error) {
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
	if err := mergo.Merge(&newConf, defaultConf); err != nil {
		return "", err
	}
	var builder strings.Builder
	for key, value := range newConf {
		builder.WriteString(key)
		builder.WriteString(" = ")
		builder.WriteString(value)
		builder.WriteString("\n")
	}
	return builder.String(), nil
}

func MergeInitData(newInitData *CasdoorInitData) (*CasdoorInitData, error) {
	if newInitData.Organizations == nil || len(newInitData.Organizations) == 0 {
		newInitData.Organizations = []Organization{
			{
				Owner:        "admin",
				Name:         "operator_default_org",
				PasswordType: "plain",
			},
		}
	} else {
		for i := 0; i < len(newInitData.Organizations); i++ {
			defaultOrg := Organization{
				Owner:        "admin",
				Name:         "operator_default_org_" + strconv.Itoa(i),
				PasswordType: "plain",
			}
			if err := mergo.Merge(&newInitData.Organizations[i], defaultOrg); err != nil {
				return nil, err
			}
		}
	}
	if newInitData.Certs == nil || len(newInitData.Certs) == 0 {
		certificate, privateKey, err := CreateJWTCertificateAndPrivateKey(newInitData.Organizations[0].Name)
		if err != nil {
			return nil, err
		}
		newInitData.Certs = []Cert{
			{
				Owner:           "admin",
				Name:            "operator_default_cert",
				Scope:           "JWT",
				Type:            "X509",
				CryptoAlgorithm: "RS256",
				BitSize:         4096,
				ExpireInYears:   20,
				Certificate:     certificate,
				PrivateKey:      privateKey,
			},
		}
	} else {
		for i := 0; i < len(newInitData.Certs); i++ {
			certificate, privateKey, err := CreateJWTCertificateAndPrivateKey(newInitData.Organizations[0].Name)
			if err != nil {
				return nil, err
			}
			defaultCert := Cert{
				Owner:           "admin",
				Name:            "operator_default_cert_" + strconv.Itoa(i),
				Scope:           "JWT",
				Type:            "X509",
				CryptoAlgorithm: "RS256",
				BitSize:         4096,
				ExpireInYears:   20,
				Certificate:     certificate,
				PrivateKey:      privateKey,
			}
			if err := mergo.Merge(&newInitData.Certs[i], defaultCert); err != nil {
				return nil, err
			}
		}
	}
	// Provider is optional, so we don't need to set default value
	var providerItems []ProviderItem
	if len(newInitData.Providers) > 0 {
		for i := 0; i < len(newInitData.Providers); i++ {
			defaultProvider := Provider{
				Owner: "admin",
				Name:  "operator_default_provider_" + strconv.Itoa(i),
			}
			if err := mergo.Merge(&newInitData.Providers[i], defaultProvider); err != nil {
				return nil, err
			}
			providerItems = append(providerItems, ProviderItem{
				Name:      newInitData.Providers[i].Name,
				CanSignUp: true,
				CanSignIn: true,
				CanUnlink: true,
				Prompted:  false,
				AlertType: "None",
			})
		}
	}
	// Application depends on Organization, Provider and Cert
	if newInitData.Applications == nil || len(newInitData.Applications) == 0 {
		clientID, err := RandomHexStr(10)
		if err != nil {
			return nil, err
		}
		clientSecret, err := RandomHexStr(20)
		if err != nil {
			return nil, err
		}
		newInitData.Applications = []Application{
			{
				Owner:          "admin",
				Name:           "operator_default_app",
				Organization:   newInitData.Organizations[0].Name,
				Cert:           newInitData.Certs[0].Name,
				ClientID:       clientID,
				ClientSecret:   clientSecret,
				RedirectUris:   []string{},
				EnablePassword: true,
				ExpireInHours:  168,
				Providers:      providerItems,
			},
		}
	} else {
		for i := 0; i < len(newInitData.Applications); i++ {
			clientID, err := RandomHexStr(10)
			if err != nil {
				return nil, err
			}
			clientSecret, err := RandomHexStr(20)
			if err != nil {
				return nil, err
			}
			defaultApp := Application{
				Owner:          "admin",
				Name:           "operator_default_app_" + strconv.Itoa(i),
				Organization:   newInitData.Organizations[0].Name,
				Cert:           newInitData.Certs[0].Name,
				ClientID:       clientID,
				ClientSecret:   clientSecret,
				RedirectUris:   []string{},
				EnablePassword: true,
				ExpireInHours:  168,
				Providers:      providerItems,
			}
			if err := mergo.Merge(&newInitData.Applications[i], defaultApp); err != nil {
				return nil, err
			}
		}
	}
	// User and Ldap is optional, so we don't need to set default value
	// User depends on Organization and Application
	if len(newInitData.Users) > 0 {
		for i := 0; i < len(newInitData.Users); i++ {
			defaultUser := User{
				Owner:             newInitData.Organizations[0].Name,
				Name:              "operator_default_user",
				Password:          "default",
				SignupApplication: newInitData.Applications[0].Name,
			}
			if err := mergo.Merge(&newInitData.Users[i], defaultUser); err != nil {
				return nil, err
			}
		}
	}
	return newInitData, nil
}

func RandomHexStr(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func CreateJWTCertificateAndPrivateKey(orgName string) (string, string, error) {
	// Generate RSA key.
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return "", "", err
	}

	// Encode private key to PKCS#1 ASN.1 PEM.
	privateKeyPem := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		},
	)

	tml := x509.Certificate{
		NotBefore: time.Now(),
		NotAfter:  time.Now().AddDate(99, 0, 0),
		// you have to generate a different serial number each execution
		SerialNumber: big.NewInt(123456),
		Subject: pkix.Name{
			CommonName:   orgName + " Cert",
			Organization: []string{orgName},
		},
		BasicConstraintsValid: true,
	}
	cert, err := x509.CreateCertificate(rand.Reader, &tml, &tml, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}

	// Generate a pem block with the certificate
	certPem := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert,
	})

	return string(certPem), string(privateKeyPem), nil
}
