package utils

import "encoding/json"

// Some structs in Casdoor cannot be marshelled, so here are simpified structs.

type CasdoorInitData struct {
	Organizations []Organization `json:"organizations,omitempty"`
	Applications  []Application  `json:"applications,omitempty"`
	Users         []User         `json:"users,omitempty"`
	Certs         []Cert         `json:"certs,omitempty"`
	Providers     []Provider     `json:"providers,omitempty"`
	Ldaps         []Ldap         `json:"ldaps,omitempty"`
}

func (d CasdoorInitData) DeepCopyInto(u *CasdoorInitData) {
	oldJson, err := json.Marshal(d)
	if err != nil {
		panic(err)
	}
	if err = json.Unmarshal(oldJson, u); err != nil {
		panic(err)
	}
}

type Organization struct {
	Owner       string `json:"owner,omitempty"`
	Name        string `json:"name"`
	CreatedTime string `json:"createdTime,omitempty"`

	DisplayName        string `json:"displayName,omitempty"`
	WebsiteUrl         string `json:"websiteUrl,omitempty"`
	Favicon            string `json:"favicon,omitempty"`
	PasswordType       string `json:"passwordType,omitempty"`
	PasswordSalt       string `json:"passwordSalt,omitempty"`
	PhonePrefix        string `json:"phonePrefix,omitempty"`
	DefaultAvatar      string `json:"defaultAvatar,omitempty"`
	MasterPassword     string `json:"masterPassword,omitempty"`
	EnableSoftDeletion bool   `json:"enableSoftDeletion,omitempty"`
}

type Application struct {
	Owner          string   `json:"owner,omitempty"`
	Name           string   `json:"name"`
	DisplayName    string   `json:"displayName,omitempty"`
	Logo           string   `json:"logo,omitempty"`
	HomepageURL    string   `json:"homepageUrl,omitempty"`
	Organization   string   `json:"organization,omitempty"`
	Cert           string   `json:"cert,omitempty"`
	EnablePassword bool     `json:"enablePassword,omitempty"`
	EnableSignUp   bool     `json:"enableSignUp,omitempty"`
	ClientID       string   `json:"clientId,omitempty"`
	ClientSecret   string   `json:"clientSecret,omitempty"`
	RedirectUris   []string `json:"redirectUris,omitempty"`
	ExpireInHours  int      `json:"expireInHours,omitempty"`

	Providers   []ProviderItem `json:"providers,omitempty"`
	SignupItems []SignupItem   `json:"signupItems,omitempty"`
}

type User struct {
	Owner       string `json:"owner,omitempty"`
	Name        string `json:"name,omitempty"`
	CreatedTime string `json:"createdTime,omitempty"`
	UpdatedTime string `json:"updatedTime,omitempty"`

	Type              string   `json:"type,omitempty"`
	Password          string   `json:"password,omitempty"`
	PasswordSalt      string   `json:"passwordSalt,omitempty"`
	DisplayName       string   `json:"displayName,omitempty"`
	Avatar            string   `json:"avatar,omitempty"`
	PermanentAvatar   string   `json:"permanentAvatar,omitempty"`
	Email             string   `json:"email,omitempty"`
	Phone             string   `json:"phone,omitempty"`
	Location          string   `json:"location,omitempty"`
	Address           []string `json:"address,omitempty"`
	Affiliation       string   `json:"affiliation,omitempty"`
	Title             string   `json:"title,omitempty"`
	IdCardType        string   `json:"idCardType,omitempty"`
	IdCard            string   `json:"idCard,omitempty"`
	Homepage          string   `json:"homepage,omitempty"`
	Bio               string   `json:"bio,omitempty"`
	Tag               string   `json:"tag,omitempty"`
	Region            string   `json:"region,omitempty"`
	Language          string   `json:"language,omitempty"`
	Gender            string   `json:"gender,omitempty"`
	Birthday          string   `json:"birthday,omitempty"`
	Education         string   `json:"education,omitempty"`
	Score             int      `json:"score,omitempty"`
	Karma             int      `json:"karma,omitempty"`
	Ranking           int      `json:"ranking,omitempty"`
	IsDefaultAvatar   bool     `json:"isDefaultAvatar,omitempty"`
	IsOnline          bool     `json:"isOnline,omitempty"`
	IsAdmin           bool     `json:"isAdmin,omitempty"`
	IsGlobalAdmin     bool     `json:"isGlobalAdmin,omitempty"`
	IsForbidden       bool     `json:"isForbidden,omitempty"`
	IsDeleted         bool     `json:"isDeleted,omitempty"`
	SignupApplication string   `json:"signupApplication,omitempty"`
	Hash              string   `json:"hash,omitempty"`
	PreHash           string   `json:"preHash,omitempty"`

	Ldap       string            `json:"ldap,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type ProviderItem struct {
	Name      string `json:"name"`
	CanSignUp bool   `json:"canSignUp,omitempty"`
	CanSignIn bool   `json:"canSignIn,omitempty"`
	CanUnlink bool   `json:"canUnlink,omitempty"`
	Prompted  bool   `json:"prompted,omitempty"`
	AlertType string `json:"alertType,omitempty"`
}

type SignupItem struct {
	Name     string `json:"name"`
	Visible  bool   `json:"visible"`
	Required bool   `json:"required"`
	Prompted bool   `json:"prompted"`
	Rule     string `json:"rule"`
}

type Cert struct {
	Owner           string `json:"owner,omitempty"`
	Name            string `json:"name"`
	DisplayName     string `json:"displayName,omitempty"`
	Scope           string `json:"scope,omitempty"`
	Type            string `json:"type,omitempty"`
	CryptoAlgorithm string `json:"cryptoAlgorithm,omitempty"`
	BitSize         int    `json:"bitSize,omitempty"`
	ExpireInYears   int    `json:"expireInYears,omitempty"`
	Certificate     string `json:"certificate,omitempty"`
	PrivateKey      string `json:"privateKey,omitempty"`
}

type Provider struct {
	Owner        string `json:"owner,omitempty"`
	Name         string `json:"name"`
	DisplayName  string `json:"displayName,omitempty"`
	Category     string `json:"category"`
	Type         string `json:"type"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

type Ldap struct {
	Id          string `json:"id"`
	Owner       string `json:"owner"`
	CreatedTime string `json:"createdTime"`

	ServerName string `json:"serverName"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Admin      string `json:"admin"`
	Passwd     string `json:"passwd"`
	BaseDn     string `json:"baseDn"`

	AutoSync int    `json:"autoSync"`
	LastSync string `json:"lastSync"`
}
